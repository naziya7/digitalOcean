package image

import (
	"bytes"
	"encoding/base64"
	"errors"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

func TestDecodeImage_PNG(t *testing.T) {
	data := makePNG(t, 40, 30)
	decoded, err := DecodeImage(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if decoded.Format != "png" || decoded.ContentType != "image/png" {
		t.Fatalf("format=%q ct=%q", decoded.Format, decoded.ContentType)
	}
	if decoded.Width != 40 || decoded.Height != 30 {
		t.Fatalf("got %dx%d", decoded.Width, decoded.Height)
	}
}

func TestDecodeImage_JPEG(t *testing.T) {
	data := makeJPEG(t, 32, 24)
	decoded, err := DecodeImage(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if decoded.Format != "jpeg" || decoded.ContentType != "image/jpeg" {
		t.Fatalf("format=%q ct=%q", decoded.Format, decoded.ContentType)
	}
}

func TestDecodeImage_WebP(t *testing.T) {
	// 1x1 lossy WebP (VP8)
	raw, err := base64.StdEncoding.DecodeString("UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAwA0JaQAA3AA/vuUAAA=")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeImage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DecodeImage webp: %v", err)
	}
	if decoded.Format != "webp" || decoded.ContentType != "image/webp" {
		t.Fatalf("format=%q ct=%q", decoded.Format, decoded.ContentType)
	}
	if decoded.Width != 1 || decoded.Height != 1 {
		t.Fatalf("got %dx%d", decoded.Width, decoded.Height)
	}
}

func TestDecodeImage_Corrupt(t *testing.T) {
	_, err := DecodeImage(bytes.NewReader([]byte("not-an-image")))
	if !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("got %v, want ErrInvalidImage", err)
	}
}

func TestGenerateThumbnail_LandscapePNG(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "thumb.png")
	data := makePNG(t, 400, 300)

	res, err := GenerateThumbnail(bytes.NewReader(data), out, 150, 150)
	if err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}
	if res.Width != 150 || res.Height != 113 {
		t.Fatalf("got %dx%d, want 150x113", res.Width, res.Height)
	}
	if res.ContentType != "image/png" {
		t.Fatalf("content type %q", res.ContentType)
	}
	if res.FileSize <= 0 {
		t.Fatalf("expected positive file size, got %d", res.FileSize)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() != res.FileSize {
		t.Fatalf("stat size %d != result %d", info.Size(), res.FileSize)
	}

	// Re-decode written thumbnail to confirm it is a real image.
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := DecodeImage(f)
	if err != nil {
		t.Fatalf("re-decode thumbnail: %v", err)
	}
	if got.Width != 150 || got.Height != 113 {
		t.Fatalf("re-decoded %dx%d", got.Width, got.Height)
	}
}

func TestGenerateThumbnail_JPEG_NoUpscale(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "thumb.jpg")
	data := makeJPEG(t, 80, 60)

	res, err := GenerateThumbnail(bytes.NewReader(data), out, 150, 150)
	if err != nil {
		t.Fatalf("GenerateThumbnail: %v", err)
	}
	if res.Width != 80 || res.Height != 60 {
		t.Fatalf("got %dx%d, want 80x60 (no upscale)", res.Width, res.Height)
	}
	if res.ContentType != "image/jpeg" {
		t.Fatalf("content type %q", res.ContentType)
	}
}

func TestGenerateThumbnail_Corrupt(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "thumb.jpg")
	_, err := GenerateThumbnail(bytes.NewReader([]byte{0x00, 0x01, 0x02}), out, 150, 150)
	if !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("got %v, want ErrInvalidImage", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("expected no output file on corrupt input")
	}
}

func TestGenerateThumbnail_InvalidBox(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "thumb.png")
	data := makePNG(t, 20, 20)
	_, err := GenerateThumbnail(bytes.NewReader(data), out, 0, 150)
	if !errors.Is(err, ErrInvalidDimensions) {
		t.Fatalf("got %v, want ErrInvalidDimensions", err)
	}
}

func TestGenerateThumbnail_PresetMedium(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "medium.png")
	data := makePNG(t, 900, 600)
	box := PresetDimensions[PresetMedium]

	res, err := GenerateThumbnail(bytes.NewReader(data), out, box.Width, box.Height)
	if err != nil {
		t.Fatal(err)
	}
	// 900x600 → 300x200
	if res.Width != 300 || res.Height != 200 {
		t.Fatalf("got %dx%d, want 300x200", res.Width, res.Height)
	}
}
