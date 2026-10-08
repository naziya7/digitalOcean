package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"image-thumbnail-api/internal/config"
	"image-thumbnail-api/internal/model"
	"image-thumbnail-api/internal/service"
)

// imageService is the service surface used by HTTP handlers.
type imageService interface {
	UploadAndResize(ctx context.Context, files []service.UploadFile, opts service.UploadOptions) (*service.UploadResult, error)
	GetImage(ctx context.Context, id string) (*model.Image, error)
	GetThumbnailMetadata(ctx context.Context, id string) (*model.Thumbnail, error)
	GetThumbnailPath(ctx context.Context, id string) (string, *model.Thumbnail, error)
}

// ImageHandler exposes HTTP endpoints for image upload and thumbnail retrieval.
type ImageHandler struct {
	svc            imageService
	maxUploadBytes int64
	maxFiles       int
}

// NewImageHandler constructs an ImageHandler.
func NewImageHandler(svc *service.ImageService, cfg config.Config) *ImageHandler {
	return newImageHandler(svc, cfg)
}

func newImageHandler(svc imageService, cfg config.Config) *ImageHandler {
	maxUpload := cfg.MaxUploadBytes
	if maxUpload <= 0 {
		maxUpload = 10 << 20
	}
	maxFiles := cfg.MaxFilesPerRequest
	if maxFiles <= 0 {
		maxFiles = 5
	}
	return &ImageHandler{
		svc:            svc,
		maxUploadBytes: maxUpload,
		maxFiles:       maxFiles,
	}
}

// RegisterRoutes mounts image-related routes on the given mux.
func (h *ImageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/images", h.Upload)
	mux.HandleFunc("GET /v1/images/{imageID}", h.GetImage)
	mux.HandleFunc("GET /v1/thumbnails/{thumbnailID}", h.GetThumbnailMetadata)
	mux.HandleFunc("GET /v1/thumbnails/{thumbnailID}/file", h.GetThumbnailFile)
}

// Upload handles POST /v1/images.
func (h *ImageHandler) Upload(w http.ResponseWriter, r *http.Request) {
	maxBody := h.maxUploadBytes*int64(h.maxFiles) + (1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)

	maxMemory := h.maxUploadBytes
	if maxMemory > 32<<20 {
		maxMemory = 32 << 20
	}
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_multipart", "malformed multipart form data")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	opts, err := parseUploadOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_options", err.Error())
		return
	}

	files, err := readUploadFiles(r, h.maxFiles, h.maxUploadBytes)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	result, err := h.svc.UploadAndResize(r.Context(), files, opts)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// GetImage handles GET /v1/images/{imageID}.
func (h *ImageHandler) GetImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("imageID")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "image id is required")
		return
	}

	img, err := h.svc.GetImage(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, img)
}

// GetThumbnailMetadata handles GET /v1/thumbnails/{thumbnailID}.
func (h *ImageHandler) GetThumbnailMetadata(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("thumbnailID")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "thumbnail id is required")
		return
	}

	thumb, err := h.svc.GetThumbnailMetadata(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, thumb)
}

// GetThumbnailFile handles GET /v1/thumbnails/{thumbnailID}/file.
func (h *ImageHandler) GetThumbnailFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("thumbnailID")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "thumbnail id is required")
		return
	}

	path, thumb, err := h.svc.GetThumbnailPath(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "not_found", "thumbnail file not found")
			return
		}
		slog.Error("open thumbnail file failed", "error", err, "path", path)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		slog.Error("stat thumbnail file failed", "error", err, "path", path)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.Header().Set("Content-Type", thumb.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		slog.Error("write thumbnail file failed", "error", err)
	}
}

func parseUploadOptions(r *http.Request) (service.UploadOptions, error) {
	var opts service.UploadOptions

	if presets := strings.TrimSpace(r.FormValue("presets")); presets != "" {
		for _, p := range strings.Split(presets, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				opts.Presets = append(opts.Presets, p)
			}
		}
	}

	widthStr := strings.TrimSpace(r.FormValue("width"))
	heightStr := strings.TrimSpace(r.FormValue("height"))
	if widthStr != "" || heightStr != "" {
		if widthStr == "" || heightStr == "" {
			return opts, errors.New("width and height must both be provided for custom dimensions")
		}
		w, err := strconv.Atoi(widthStr)
		if err != nil {
			return opts, errors.New("width must be an integer")
		}
		h, err := strconv.Atoi(heightStr)
		if err != nil {
			return opts, errors.New("height must be an integer")
		}
		opts.CustomWidth = w
		opts.CustomHeight = h
	}

	return opts, nil
}

func readUploadFiles(r *http.Request, maxFiles int, maxUploadBytes int64) ([]service.UploadFile, error) {
	if r.MultipartForm == nil {
		return nil, fmt.Errorf("%w: missing multipart form", service.ErrInvalidOptions)
	}

	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		return nil, fmt.Errorf("%w: at least one file is required in field \"files\"", service.ErrInvalidOptions)
	}
	if len(headers) > maxFiles {
		return nil, service.ErrTooManyFiles
	}

	out := make([]service.UploadFile, 0, len(headers))
	for _, fh := range headers {
		if fh.Size > maxUploadBytes {
			return nil, service.ErrFileTooLarge
		}
		f, err := fh.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: unable to read uploaded file", service.ErrInvalidOptions)
		}

		limited := io.LimitReader(f, maxUploadBytes+1)
		data, err := io.ReadAll(limited)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: unable to read uploaded file", service.ErrInvalidOptions)
		}
		if int64(len(data)) > maxUploadBytes {
			return nil, service.ErrFileTooLarge
		}
		if len(data) == 0 {
			return nil, service.ErrEmptyFile
		}

		out = append(out, service.UploadFile{
			Filename: fh.Filename,
			Data:     data,
		})
	}
	return out, nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrFileTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "file exceeds maximum upload size")
	case errors.Is(err, service.ErrTooManyFiles):
		writeError(w, http.StatusBadRequest, "too_many_files", "too many files in request")
	case errors.Is(err, service.ErrEmptyFile):
		writeError(w, http.StatusBadRequest, "empty_file", "empty file")
	case errors.Is(err, service.ErrUnsupportedType):
		writeError(w, http.StatusBadRequest, "unsupported_type", "unsupported image type")
	case errors.Is(err, service.ErrInvalidImage):
		writeError(w, http.StatusBadRequest, "invalid_image", "invalid or corrupt image data")
	case errors.Is(err, service.ErrImageTooLarge):
		writeError(w, http.StatusBadRequest, "image_too_large", "image exceeds maximum pixel limit")
	case errors.Is(err, service.ErrInvalidOptions):
		writeError(w, http.StatusBadRequest, "invalid_options", "invalid upload options")
	case errors.Is(err, service.ErrImageNotFound), errors.Is(err, service.ErrThumbnailNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, context.Canceled):
		writeError(w, http.StatusBadRequest, "request_canceled", "request canceled")
	default:
		slog.Error("request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
