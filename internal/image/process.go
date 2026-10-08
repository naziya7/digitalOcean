package image

import (
	"errors"
	"fmt"
	stdimage "image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"

	// Register decoders for image.Decode.
	_ "image/gif"
	_ "golang.org/x/image/webp"
)

// ErrInvalidImage is returned when input bytes cannot be decoded as an image.
var ErrInvalidImage = errors.New("invalid or corrupt image data")

// ThumbnailResult is metadata about a generated thumbnail file.
type ThumbnailResult struct {
	Width       int
	Height      int
	ContentType string
	FileSize    int64
}

// DecodedImage holds a validated in-memory image and its format info.
type DecodedImage struct {
	Image       stdimage.Image
	Format      string // jpeg | png | webp | gif
	Width       int
	Height      int
	ContentType string
}

// DecodeImage decodes and validates image data from r.
// It returns ErrInvalidImage when the payload is not a supported image.
func DecodeImage(r io.Reader) (*DecodedImage, error) {
	img, format, err := stdimage.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%w: empty image bounds", ErrInvalidImage)
	}

	ct, err := contentTypeForFormat(format)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}

	return &DecodedImage{
		Image:       img,
		Format:      format,
		Width:       w,
		Height:      h,
		ContentType: ct,
	}, nil
}

// GenerateThumbnail decodes an image from r, resizes it with FitSize into the
// requested bounding box, encodes it, and writes the file to outputPath.
// Aspect ratio is preserved and the image is never upscaled.
func GenerateThumbnail(r io.Reader, outputPath string, requestedWidth, requestedHeight int) (ThumbnailResult, error) {
	decoded, err := DecodeImage(r)
	if err != nil {
		return ThumbnailResult{}, err
	}
	return GenerateThumbnailFromDecoded(decoded, outputPath, requestedWidth, requestedHeight)
}

// GenerateThumbnailFromDecoded resizes an already-decoded image and writes it to outputPath.
func GenerateThumbnailFromDecoded(decoded *DecodedImage, outputPath string, requestedWidth, requestedHeight int) (ThumbnailResult, error) {
	if decoded == nil || decoded.Image == nil {
		return ThumbnailResult{}, fmt.Errorf("%w: nil image", ErrInvalidImage)
	}

	outW, outH, err := FitSize(decoded.Width, decoded.Height, requestedWidth, requestedHeight)
	if err != nil {
		return ThumbnailResult{}, err
	}

	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, outW, outH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), decoded.Image, decoded.Image.Bounds(), xdraw.Over, nil)

	encodeFormat, contentType := outputFormat(decoded.Format)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return ThumbnailResult{}, fmt.Errorf("create thumbnail directory: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return ThumbnailResult{}, fmt.Errorf("create thumbnail file: %w", err)
	}
	defer f.Close()

	if err := encodeImage(f, dst, encodeFormat); err != nil {
		_ = os.Remove(outputPath)
		return ThumbnailResult{}, fmt.Errorf("encode thumbnail: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = os.Remove(outputPath)
		return ThumbnailResult{}, fmt.Errorf("sync thumbnail file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		return ThumbnailResult{}, fmt.Errorf("stat thumbnail file: %w", err)
	}

	return ThumbnailResult{
		Width:       outW,
		Height:      outH,
		ContentType: contentType,
		FileSize:    info.Size(),
	}, nil
}

func contentTypeForFormat(format string) (string, error) {
	switch strings.ToLower(format) {
	case "jpeg":
		return "image/jpeg", nil
	case "png":
		return "image/png", nil
	case "webp":
		return "image/webp", nil
	case "gif":
		return "image/gif", nil
	default:
		return "", fmt.Errorf("unsupported image format %q", format)
	}
}

// outputFormat chooses an encode format. WebP/GIF are re-encoded as JPEG
// because pure-Go WebP encoding is not available without CGo.
func outputFormat(srcFormat string) (encodeFormat, contentType string) {
	switch strings.ToLower(srcFormat) {
	case "png":
		return "png", "image/png"
	default:
		return "jpeg", "image/jpeg"
	}
}

func encodeImage(w io.Writer, img stdimage.Image, format string) error {
	switch format {
	case "png":
		return png.Encode(w, img)
	case "jpeg":
		return jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	default:
		return fmt.Errorf("unsupported encode format %q", format)
	}
}
