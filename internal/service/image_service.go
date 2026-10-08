package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"image-thumbnail-api/internal/config"
	imgproc "image-thumbnail-api/internal/image"
	"image-thumbnail-api/internal/model"
	"image-thumbnail-api/internal/repository"
)

// Sentinel errors for validation and lookup failures.
var (
	ErrTooManyFiles     = errors.New("too many files in request")
	ErrFileTooLarge     = errors.New("file exceeds maximum upload size")
	ErrEmptyFile        = errors.New("empty file")
	ErrUnsupportedType  = errors.New("unsupported image type")
	ErrInvalidOptions   = errors.New("invalid upload options")
	ErrInvalidImage     = imgproc.ErrInvalidImage
	ErrImageTooLarge     = errors.New("image exceeds maximum pixel limit")
	ErrImageNotFound     = errors.New("image not found")
	ErrThumbnailNotFound = errors.New("thumbnail not found")
)

// imageStore is the persistence surface the service needs.
type imageStore interface {
	CreateImage(ctx context.Context, img *model.Image) error
	GetImageByID(ctx context.Context, id string) (*model.Image, error)
	DeleteImage(ctx context.Context, id string) error
	GetImageByThumbnailID(ctx context.Context, thumbnailID string) (*model.Image, error)
}

// ImageService coordinates validation, resizing, storage, and metadata persistence.
type ImageService struct {
	repo    imageStore
	cfg     config.Config
	dataDir string
}

// NewImageService constructs an ImageService.
func NewImageService(repo *repository.ImageRepository, cfg config.Config) *ImageService {
	return newImageService(repo, cfg)
}

func newImageService(repo imageStore, cfg config.Config) *ImageService {
	return &ImageService{
		repo:    repo,
		cfg:     cfg,
		dataDir: cfg.DataDir,
	}
}

// UploadFile is one in-memory upload (HTTP layer fills this from multipart).
type UploadFile struct {
	Filename string
	Data     []byte
}

// UploadOptions controls which thumbnails are generated.
// If Presets is empty and no custom size is set, small/medium/large are used.
type UploadOptions struct {
	Presets      []string // small | medium | large
	CustomWidth  int
	CustomHeight int
}

// UploadResult is returned after processing an upload request.
type UploadResult struct {
	Images []model.Image `json:"images"`
}

type thumbTarget struct {
	preset string
	width  int
	height int
}

// UploadAndResize validates files, writes originals and thumbnails, and persists metadata.
func (s *ImageService) UploadAndResize(ctx context.Context, files []UploadFile, opts UploadOptions) (*UploadResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	maxFiles := s.cfg.MaxFilesPerRequest
	if maxFiles <= 0 {
		maxFiles = 5
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: at least one file is required", ErrInvalidOptions)
	}
	if len(files) > maxFiles {
		return nil, fmt.Errorf("%w: max %d files allowed", ErrTooManyFiles, maxFiles)
	}

	targets, err := s.resolveTargets(opts)
	if err != nil {
		return nil, err
	}

	var (
		writtenPaths []string
		createdIDs   []string
		results      []model.Image
	)

	cleanup := func() {
		for _, p := range writtenPaths {
			_ = os.Remove(p)
		}
		for _, id := range createdIDs {
			_ = s.repo.DeleteImage(context.Background(), id)
		}
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			cleanup()
			return nil, err
		}

		img, paths, err := s.processOne(ctx, file, targets)
		if err != nil {
			writtenPaths = append(writtenPaths, paths...)
			cleanup()
			return nil, err
		}
		writtenPaths = append(writtenPaths, paths...)

		if err := s.repo.CreateImage(ctx, img); err != nil {
			cleanup()
			return nil, fmt.Errorf("save image metadata: %w", err)
		}
		createdIDs = append(createdIDs, img.ID)
		results = append(results, *img)
	}

	return &UploadResult{Images: results}, nil
}

func (s *ImageService) processOne(ctx context.Context, file UploadFile, targets []thumbTarget) (*model.Image, []string, error) {
	var written []string

	if len(file.Data) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrEmptyFile, file.Filename)
	}

	maxBytes := s.cfg.MaxUploadBytes
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if int64(len(file.Data)) > maxBytes {
		return nil, nil, fmt.Errorf("%w: %s (%d bytes)", ErrFileTooLarge, file.Filename, len(file.Data))
	}

	decoded, err := imgproc.DecodeImage(bytes.NewReader(file.Data))
	if err != nil {
		if errors.Is(err, imgproc.ErrInvalidImage) {
			return nil, nil, fmt.Errorf("%w: %s", ErrInvalidImage, file.Filename)
		}
		return nil, nil, err
	}

	if err := s.validateDecoded(decoded); err != nil {
		return nil, nil, fmt.Errorf("%w: %s", err, file.Filename)
	}

	imageID := newID("img")
	now := time.Now().UTC()
	ext := extensionForFormat(decoded.Format)
	originalPath := filepath.Join(s.dataDir, "originals", imageID+ext)

	if err := ctx.Err(); err != nil {
		return nil, written, err
	}

	if err := os.MkdirAll(filepath.Dir(originalPath), 0o755); err != nil {
		return nil, written, fmt.Errorf("create originals directory: %w", err)
	}
	if err := os.WriteFile(originalPath, file.Data, 0o644); err != nil {
		return nil, written, fmt.Errorf("write original file: %w", err)
	}
	written = append(written, originalPath)

	thumbnails := make([]model.Thumbnail, 0, len(targets))
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, written, err
		}

		thumbID := newID("thm")
		outExt := extensionForContentType(outputContentType(decoded.Format))
		thumbPath := filepath.Join(s.dataDir, "thumbnails", thumbID+outExt)

		res, err := imgproc.GenerateThumbnailFromDecoded(decoded, thumbPath, target.width, target.height)
		if err != nil {
			return nil, written, fmt.Errorf("generate %s thumbnail for %s: %w", target.preset, file.Filename, err)
		}
		written = append(written, thumbPath)

		thumbnails = append(thumbnails, model.Thumbnail{
			ID:              thumbID,
			ImageID:         imageID,
			Preset:          target.preset,
			RequestedWidth:  target.width,
			RequestedHeight: target.height,
			ActualWidth:     res.Width,
			ActualHeight:    res.Height,
			ContentType:     res.ContentType,
			FileSize:        res.FileSize,
			FilePath:        thumbPath,
			CreatedAt:       now,
		})
	}

	img := &model.Image{
		ID:               imageID,
		OriginalFilename: file.Filename,
		ContentType:      decoded.ContentType,
		OriginalWidth:    decoded.Width,
		OriginalHeight:   decoded.Height,
		OriginalSize:     int64(len(file.Data)),
		OriginalPath:     originalPath,
		CreatedAt:        now,
		Thumbnails:       thumbnails,
	}
	return img, written, nil
}

func (s *ImageService) validateDecoded(decoded *imgproc.DecodedImage) error {
	switch decoded.Format {
	case "jpeg", "png", "webp", "gif":
		// ok
	default:
		return ErrUnsupportedType
	}

	maxPixels := s.cfg.MaxImagePixels
	if maxPixels <= 0 {
		maxPixels = 25_000_000
	}
	pixels := int64(decoded.Width) * int64(decoded.Height)
	if pixels > maxPixels {
		return ErrImageTooLarge
	}
	return nil
}

func (s *ImageService) resolveTargets(opts UploadOptions) ([]thumbTarget, error) {
	maxDim := s.cfg.MaxCustomDimension
	if maxDim <= 0 {
		maxDim = 2000
	}

	hasCustom := opts.CustomWidth != 0 || opts.CustomHeight != 0
	if hasCustom {
		if opts.CustomWidth <= 0 || opts.CustomHeight <= 0 {
			return nil, fmt.Errorf("%w: custom width and height must both be positive", ErrInvalidOptions)
		}
		if opts.CustomWidth > maxDim || opts.CustomHeight > maxDim {
			return nil, fmt.Errorf("%w: custom dimensions must be <= %d", ErrInvalidOptions, maxDim)
		}
	}

	var targets []thumbTarget

	presets := opts.Presets
	if len(presets) == 0 && !hasCustom {
		presets = []string{
			string(imgproc.PresetSmall),
			string(imgproc.PresetMedium),
			string(imgproc.PresetLarge),
		}
	}

	seen := make(map[string]struct{})
	for _, name := range presets {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		preset := imgproc.Preset(key)
		box, ok := imgproc.PresetDimensions[preset]
		if !ok {
			return nil, fmt.Errorf("%w: unknown preset %q", ErrInvalidOptions, name)
		}
		seen[key] = struct{}{}
		targets = append(targets, thumbTarget{
			preset: key,
			width:  box.Width,
			height: box.Height,
		})
	}

	if hasCustom {
		targets = append(targets, thumbTarget{
			preset: string(imgproc.PresetCustom),
			width:  opts.CustomWidth,
			height: opts.CustomHeight,
		})
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: no thumbnail sizes selected", ErrInvalidOptions)
	}
	return targets, nil
}

// GetThumbnailMetadata returns thumbnail metadata by thumbnail ID.
func (s *ImageService) GetThumbnailMetadata(ctx context.Context, id string) (*model.Thumbnail, error) {
	if id == "" {
		return nil, ErrThumbnailNotFound
	}
	img, err := s.repo.GetImageByThumbnailID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrThumbnailNotFound
		}
		return nil, err
	}
	for i := range img.Thumbnails {
		if img.Thumbnails[i].ID == id {
			thumb := img.Thumbnails[i]
			return &thumb, nil
		}
	}
	return nil, ErrThumbnailNotFound
}

// GetThumbnailPath returns the filesystem path and metadata for a thumbnail file.
func (s *ImageService) GetThumbnailPath(ctx context.Context, id string) (string, *model.Thumbnail, error) {
	thumb, err := s.GetThumbnailMetadata(ctx, id)
	if err != nil {
		return "", nil, err
	}
	return thumb.FilePath, thumb, nil
}

// GetImage returns image metadata by ID.
func (s *ImageService) GetImage(ctx context.Context, id string) (*model.Image, error) {
	img, err := s.repo.GetImageByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrImageNotFound
		}
		return nil, err
	}
	return img, nil
}

func newID(prefix string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}

func extensionForFormat(format string) string {
	switch strings.ToLower(format) {
	case "jpeg":
		return ".jpg"
	case "png":
		return ".png"
	case "webp":
		return ".webp"
	case "gif":
		return ".gif"
	default:
		return ".bin"
	}
}

func outputContentType(srcFormat string) string {
	if strings.ToLower(srcFormat) == "png" {
		return "image/png"
	}
	return "image/jpeg"
}

func extensionForContentType(ct string) string {
	switch ct {
	case "image/png":
		return ".png"
	default:
		return ".jpg"
	}
}
