package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"image-thumbnail-api/internal/config"
	"image-thumbnail-api/internal/model"
	"image-thumbnail-api/internal/service"
)

type mockService struct {
	uploadFn   func(ctx context.Context, files []service.UploadFile, opts service.UploadOptions) (*service.UploadResult, error)
	getImageFn func(ctx context.Context, id string) (*model.Image, error)
	getThumbFn func(ctx context.Context, id string) (*model.Thumbnail, error)
	getPathFn  func(ctx context.Context, id string) (string, *model.Thumbnail, error)
}

func (m *mockService) UploadAndResize(ctx context.Context, files []service.UploadFile, opts service.UploadOptions) (*service.UploadResult, error) {
	return m.uploadFn(ctx, files, opts)
}

func (m *mockService) GetImage(ctx context.Context, id string) (*model.Image, error) {
	return m.getImageFn(ctx, id)
}

func (m *mockService) GetThumbnailMetadata(ctx context.Context, id string) (*model.Thumbnail, error) {
	return m.getThumbFn(ctx, id)
}

func (m *mockService) GetThumbnailPath(ctx context.Context, id string) (string, *model.Thumbnail, error) {
	return m.getPathFn(ctx, id)
}

func testHandler(svc imageService) (*ImageHandler, *http.ServeMux) {
	h := newImageHandler(svc, config.Config{
		MaxUploadBytes:     10 << 20,
		MaxFilesPerRequest: 5,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multipartBody(t *testing.T, fields map[string]string, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		part, err := w.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestUpload_Created(t *testing.T) {
	pngData := makePNG(t, 20, 10)
	svc := &mockService{
		uploadFn: func(_ context.Context, files []service.UploadFile, opts service.UploadOptions) (*service.UploadResult, error) {
			if len(files) != 1 || files[0].Filename != "a.png" {
				t.Fatalf("files=%+v", files)
			}
			if len(opts.Presets) != 1 || opts.Presets[0] != "small" {
				t.Fatalf("opts=%+v", opts)
			}
			if opts.CustomWidth != 100 || opts.CustomHeight != 80 {
				t.Fatalf("custom=%dx%d", opts.CustomWidth, opts.CustomHeight)
			}
			return &service.UploadResult{Images: []model.Image{{ID: "img_1", OriginalFilename: "a.png"}}}, nil
		},
	}
	_, mux := testHandler(svc)

	body, ct := multipartBody(t, map[string]string{
		"presets": "small",
		"width":   "100",
		"height":  "80",
	}, map[string][]byte{"a.png": pngData})

	req := httptest.NewRequest(http.MethodPost, "/v1/images", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ctHdr := rr.Header().Get("Content-Type"); !strings.HasPrefix(ctHdr, "application/json") {
		t.Fatalf("content-type=%q", ctHdr)
	}
	var got service.UploadResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Images) != 1 || got.Images[0].ID != "img_1" {
		t.Fatalf("got %+v", got)
	}
}

func TestUpload_MalformedMultipart(t *testing.T) {
	_, mux := testHandler(&mockService{})
	req := httptest.NewRequest(http.MethodPost, "/v1/images", strings.NewReader("not-multipart"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=abc")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestUpload_MissingFiles(t *testing.T) {
	_, mux := testHandler(&mockService{
		uploadFn: func(context.Context, []service.UploadFile, service.UploadOptions) (*service.UploadResult, error) {
			t.Fatal("should not call service")
			return nil, nil
		},
	})
	body, ct := multipartBody(t, map[string]string{"presets": "small"}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/images", body)
	req.Header.Set("Content-Type", ct)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUpload_MapsServiceErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"too large", service.ErrFileTooLarge, http.StatusRequestEntityTooLarge, "payload_too_large"},
		{"invalid image", service.ErrInvalidImage, http.StatusBadRequest, "invalid_image"},
		{"internal", errorsNew("db down"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockService{
				uploadFn: func(context.Context, []service.UploadFile, service.UploadOptions) (*service.UploadResult, error) {
					return nil, tc.err
				},
			}
			_, mux := testHandler(svc)
			pngData := makePNG(t, 8, 8)
			body, ct := multipartBody(t, nil, map[string][]byte{"a.png": pngData})
			req := httptest.NewRequest(http.MethodPost, "/v1/images", body)
			req.Header.Set("Content-Type", ct)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			var payload map[string]any
			_ = json.Unmarshal(rr.Body.Bytes(), &payload)
			errObj := payload["error"].(map[string]any)
			if errObj["code"] != tc.code {
				t.Fatalf("code=%v", errObj["code"])
			}
			if tc.status == http.StatusInternalServerError && strings.Contains(rr.Body.String(), "db down") {
				t.Fatal("internal error leaked")
			}
		})
	}
}

func errorsNew(msg string) error { return &simpleErr{msg} }

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }

func TestGetImage_OKAndNotFound(t *testing.T) {
	svc := &mockService{
		getImageFn: func(_ context.Context, id string) (*model.Image, error) {
			if id == "missing" {
				return nil, service.ErrImageNotFound
			}
			return &model.Image{ID: id}, nil
		},
	}
	_, mux := testHandler(svc)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/images/img_1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/images/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestGetThumbnailMetadata_NotFound(t *testing.T) {
	svc := &mockService{
		getThumbFn: func(context.Context, string) (*model.Thumbnail, error) {
			return nil, service.ErrThumbnailNotFound
		},
	}
	_, mux := testHandler(svc)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/thumbnails/thm_x", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestGetThumbnailFile_ServesBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.png")
	data := makePNG(t, 4, 4)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &mockService{
		getPathFn: func(context.Context, string) (string, *model.Thumbnail, error) {
			return path, &model.Thumbnail{
				ID:          "thm_1",
				ContentType: "image/png",
				FileSize:    int64(len(data)),
				FilePath:    path,
			}, nil
		},
	}
	_, mux := testHandler(svc)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/thumbnails/thm_1/file", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content-type=%q", rr.Header().Get("Content-Type"))
	}
	got, _ := io.ReadAll(rr.Body)
	if !bytes.Equal(got, data) {
		t.Fatalf("body mismatch len=%d", len(got))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	_, mux := testHandler(&mockService{})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/images", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rr.Code)
	}
}
