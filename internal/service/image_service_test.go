package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"image-thumbnail-api/internal/config"
	"image-thumbnail-api/internal/model"
	"image-thumbnail-api/internal/repository"
)

type memRepo struct {
	mu     sync.Mutex
	images map[string]*model.Image
}

func newMemRepo() *memRepo {
	return &memRepo{images: make(map[string]*model.Image)}
}

func (m *memRepo) CreateImage(_ context.Context, img *model.Image) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *img
	cp.Thumbnails = append([]model.Thumbnail(nil), img.Thumbnails...)
	m.images[img.ID] = &cp
	return nil
}

func (m *memRepo) GetImageByID(_ context.Context, id string) (*model.Image, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	img, ok := m.images[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *img
	cp.Thumbnails = append([]model.Thumbnail(nil), img.Thumbnails...)
	return &cp, nil
}

func (m *memRepo) DeleteImage(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.images[id]; !ok {
		return repository.ErrNotFound
	}
	delete(m.images, id)
	return nil
}

func (m *memRepo) GetImageByThumbnailID(_ context.Context, thumbnailID string) (*model.Image, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, img := range m.images {
		for _, th := range img.Thumbnails {
			if th.ID == thumbnailID {
				cp := *img
				cp.Thumbnails = append([]model.Thumbnail(nil), img.Thumbnails...)
				return &cp, nil
			}
		}
	}
	return nil, repository.ErrNotFound
}

func (m *memRepo) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.images)
}

func testConfig(dataDir string) config.Config {
	return config.Config{
		DataDir:            dataDir,
		MaxUploadBytes:     10 << 20,
		MaxFilesPerRequest: 5,
		MaxImagePixels:     25_000_000,
		MaxCustomDimension: 2000,
	}
}

func makePNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUploadAndResize_SuccessDefaults(t *testing.T) {
	dir := t.TempDir()
	repo := newMemRepo()
	svc := newImageService(repo, testConfig(dir))

	data := makePNGBytes(t, 400, 300)
	res, err := svc.UploadAndResize(context.Background(), []UploadFile{{
		Filename: "photo.png",
		Data:     data,
	}}, UploadOptions{})
	if err != nil {
		t.Fatalf("UploadAndResize: %v", err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("got %d images", len(res.Images))
	}
	img := res.Images[0]
	if len(img.Thumbnails) != 3 {
		t.Fatalf("want 3 default thumbnails, got %d", len(img.Thumbnails))
	}
	if _, err := os.Stat(img.OriginalPath); err != nil {
		t.Fatalf("original missing: %v", err)
	}
	for _, th := range img.Thumbnails {
		if _, err := os.Stat(th.FilePath); err != nil {
			t.Fatalf("thumbnail %s missing: %v", th.Preset, err)
		}
	}
	if repo.len() != 1 {
		t.Fatalf("repo len=%d", repo.len())
	}

	// small preset on 400x300 → 150x113
	var small *model.Thumbnail
	for i := range img.Thumbnails {
		if img.Thumbnails[i].Preset == "small" {
			small = &img.Thumbnails[i]
			break
		}
	}
	if small == nil || small.ActualWidth != 150 || small.ActualHeight != 113 {
		t.Fatalf("small thumbnail = %+v", small)
	}
}

func TestUploadAndResize_CustomAndPresets(t *testing.T) {
	dir := t.TempDir()
	svc := newImageService(newMemRepo(), testConfig(dir))
	data := makePNGBytes(t, 800, 600)

	res, err := svc.UploadAndResize(context.Background(), []UploadFile{{
		Filename: "a.png",
		Data:     data,
	}}, UploadOptions{
		Presets:      []string{"small"},
		CustomWidth:  100,
		CustomHeight: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images[0].Thumbnails) != 2 {
		t.Fatalf("want 2 thumbnails, got %d", len(res.Images[0].Thumbnails))
	}
}

func TestUploadAndResize_Validation(t *testing.T) {
	dir := t.TempDir()
	svc := newImageService(newMemRepo(), testConfig(dir))
	ctx := context.Background()
	good := makePNGBytes(t, 20, 20)

	t.Run("too many files", func(t *testing.T) {
		files := make([]UploadFile, 6)
		for i := range files {
			files[i] = UploadFile{Filename: "a.png", Data: good}
		}
		_, err := svc.UploadAndResize(ctx, files, UploadOptions{})
		if !errors.Is(err, ErrTooManyFiles) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		_, err := svc.UploadAndResize(ctx, []UploadFile{{Filename: "e.png", Data: nil}}, UploadOptions{})
		if !errors.Is(err, ErrEmptyFile) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("too large", func(t *testing.T) {
		cfg := testConfig(dir)
		cfg.MaxUploadBytes = 10
		svcLarge := newImageService(newMemRepo(), cfg)
		_, err := svcLarge.UploadAndResize(ctx, []UploadFile{{Filename: "b.png", Data: good}}, UploadOptions{})
		if !errors.Is(err, ErrFileTooLarge) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		_, err := svc.UploadAndResize(ctx, []UploadFile{{
			Filename: "bad.png",
			Data:     []byte("not-an-image"),
		}}, UploadOptions{})
		if !errors.Is(err, ErrInvalidImage) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("bad custom", func(t *testing.T) {
		_, err := svc.UploadAndResize(ctx, []UploadFile{{Filename: "a.png", Data: good}}, UploadOptions{
			CustomWidth: 100,
		})
		if !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestUploadAndResize_CleanupOnLaterFailure(t *testing.T) {
	dir := t.TempDir()
	repo := &failAfterCreateRepo{mem: newMemRepo(), failOn: 2}
	svc := newImageService(repo, testConfig(dir))
	good := makePNGBytes(t, 40, 30)

	_, err := svc.UploadAndResize(context.Background(), []UploadFile{
		{Filename: "one.png", Data: good},
		{Filename: "two.png", Data: good},
	}, UploadOptions{Presets: []string{"small"}})
	if err == nil {
		t.Fatal("expected error")
	}

	// First image metadata should be rolled back.
	if repo.mem.len() != 0 {
		t.Fatalf("expected empty repo after cleanup, len=%d", repo.mem.len())
	}

	// No leftover files under data dir.
	originals, _ := os.ReadDir(filepath.Join(dir, "originals"))
	thumbs, _ := os.ReadDir(filepath.Join(dir, "thumbnails"))
	if len(originals) != 0 || len(thumbs) != 0 {
		t.Fatalf("leftover files: originals=%d thumbs=%d", len(originals), len(thumbs))
	}
}

type failAfterCreateRepo struct {
	mem    *memRepo
	failOn int
	count  int
}

func (f *failAfterCreateRepo) CreateImage(ctx context.Context, img *model.Image) error {
	f.count++
	if f.count >= f.failOn {
		return errors.New("simulated db failure")
	}
	return f.mem.CreateImage(ctx, img)
}

func (f *failAfterCreateRepo) GetImageByID(ctx context.Context, id string) (*model.Image, error) {
	return f.mem.GetImageByID(ctx, id)
}

func (f *failAfterCreateRepo) DeleteImage(ctx context.Context, id string) error {
	return f.mem.DeleteImage(ctx, id)
}

func (f *failAfterCreateRepo) GetImageByThumbnailID(ctx context.Context, id string) (*model.Image, error) {
	return f.mem.GetImageByThumbnailID(ctx, id)
}

func TestGetThumbnailMetadata(t *testing.T) {
	dir := t.TempDir()
	repo := newMemRepo()
	svc := newImageService(repo, testConfig(dir))
	data := makePNGBytes(t, 50, 40)

	res, err := svc.UploadAndResize(context.Background(), []UploadFile{{
		Filename: "x.png",
		Data:     data,
	}}, UploadOptions{Presets: []string{"small"}})
	if err != nil {
		t.Fatal(err)
	}
	thumbID := res.Images[0].Thumbnails[0].ID

	got, err := svc.GetThumbnailMetadata(context.Background(), thumbID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != thumbID || got.Preset != "small" {
		t.Fatalf("got %+v", got)
	}

	path, meta, err := svc.GetThumbnailPath(context.Background(), thumbID)
	if err != nil {
		t.Fatal(err)
	}
	if path != meta.FilePath {
		t.Fatalf("path mismatch")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestUploadAndResize_ConcurrentRequests(t *testing.T) {
	dir := t.TempDir()
	repo := newMemRepo()
	svc := newImageService(repo, testConfig(dir))
	data := makePNGBytes(t, 60, 40)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.UploadAndResize(context.Background(), []UploadFile{{
				Filename: "c.png",
				Data:     data,
			}}, UploadOptions{Presets: []string{"small"}})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if repo.len() != 8 {
		t.Fatalf("repo len=%d want 8", repo.len())
	}
}
