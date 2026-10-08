package image

import "errors"

// Preset is a named thumbnail size.
type Preset string

const (
	PresetSmall  Preset = "small"
	PresetMedium Preset = "medium"
	PresetLarge  Preset = "large"
	PresetCustom Preset = "custom"
)

// ErrInvalidDimensions is returned when any dimension is zero or negative.
var ErrInvalidDimensions = errors.New("invalid dimensions: width and height must be positive")

// Dimensions is a target bounding box for resizing.
type Dimensions struct {
	Width  int
	Height int
}

// PresetDimensions maps preset names to target boxes.
// Actual output keeps the source aspect ratio and fits inside the box.
var PresetDimensions = map[Preset]Dimensions{
	PresetSmall:  {Width: 150, Height: 150},
	PresetMedium: {Width: 300, Height: 300},
	PresetLarge:  {Width: 600, Height: 600},
}

// FitSize returns the output width and height that fit inside the requested
// bounding box while preserving aspect ratio. The image is never upscaled.
// Presets and custom sizes both use this function with their target box.
func FitSize(srcWidth, srcHeight, requestedWidth, requestedHeight int) (int, int, error) {
	if srcWidth <= 0 || srcHeight <= 0 || requestedWidth <= 0 || requestedHeight <= 0 {
		return 0, 0, ErrInvalidDimensions
	}

	// Already fits in the box: keep original size (no upscale, no shrink).
	if srcWidth <= requestedWidth && srcHeight <= requestedHeight {
		return srcWidth, srcHeight, nil
	}

	scaleW := float64(requestedWidth) / float64(srcWidth)
	scaleH := float64(requestedHeight) / float64(srcHeight)
	scale := min(scaleW, scaleH)

	outW := int(float64(srcWidth)*scale + 0.5)
	outH := int(float64(srcHeight)*scale + 0.5)

	if outW < 1 {
		outW = 1
	}
	if outH < 1 {
		outH = 1
	}
	if outW > requestedWidth {
		outW = requestedWidth
	}
	if outH > requestedHeight {
		outH = requestedHeight
	}

	return outW, outH, nil
}
