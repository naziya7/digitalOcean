package image

import (
	"errors"
	"testing"
)

func TestFitSize_Landscape(t *testing.T) {
	// 4000x3000 into 150x150 → width-limited: 150x113
	w, h, err := FitSize(4000, 3000, 150, 150)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 150 || h != 113 {
		t.Fatalf("got %dx%d, want 150x113", w, h)
	}
}

func TestFitSize_Portrait(t *testing.T) {
	// 3000x4000 into 150x150 → height-limited: 113x150
	w, h, err := FitSize(3000, 4000, 150, 150)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 113 || h != 150 {
		t.Fatalf("got %dx%d, want 113x150", w, h)
	}
}

func TestFitSize_Square(t *testing.T) {
	w, h, err := FitSize(2000, 2000, 300, 300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 300 || h != 300 {
		t.Fatalf("got %dx%d, want 300x300", w, h)
	}
}

func TestFitSize_SmallerThanRequested_NoUpscale(t *testing.T) {
	w, h, err := FitSize(80, 60, 150, 150)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 80 || h != 60 {
		t.Fatalf("got %dx%d, want 80x60 (no upscale)", w, h)
	}
}

func TestFitSize_InvalidDimensions(t *testing.T) {
	cases := []struct {
		name                   string
		srcW, srcH, reqW, reqH int
	}{
		{"zero source width", 0, 100, 150, 150},
		{"zero source height", 100, 0, 150, 150},
		{"negative source", -1, 100, 150, 150},
		{"zero requested width", 100, 100, 0, 150},
		{"zero requested height", 100, 100, 150, 0},
		{"negative requested", 100, 100, -10, 150},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, h, err := FitSize(tc.srcW, tc.srcH, tc.reqW, tc.reqH)
			if !errors.Is(err, ErrInvalidDimensions) {
				t.Fatalf("got err=%v w=%d h=%d, want ErrInvalidDimensions", err, w, h)
			}
			if w != 0 || h != 0 {
				t.Fatalf("got %dx%d, want 0x0 on error", w, h)
			}
		})
	}
}

func TestFitSize_PreservesAspectRatio(t *testing.T) {
	srcW, srcH := 4000, 3000
	box := PresetDimensions[PresetLarge] // 600x600

	w, h, err := FitSize(srcW, srcH, box.Width, box.Height)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 4000:3000 = 4:3 → 600x450
	if w != 600 || h != 450 {
		t.Fatalf("got %dx%d, want 600x450", w, h)
	}

	srcRatio := float64(srcW) / float64(srcH)
	outRatio := float64(w) / float64(h)
	const tolerance = 0.01
	if diff := abs(srcRatio - outRatio); diff > tolerance {
		t.Fatalf("aspect ratio changed: src=%.4f out=%.4f diff=%.4f", srcRatio, outRatio, diff)
	}
}

func TestFitSize_PresetsAndCustom(t *testing.T) {
	srcW, srcH := 1920, 1080

	t.Run("small preset", func(t *testing.T) {
		box := PresetDimensions[PresetSmall]
		w, h, err := FitSize(srcW, srcH, box.Width, box.Height)
		if err != nil {
			t.Fatal(err)
		}
		// 1920x1080 → 150x84 (16:9 into 150x150)
		if w != 150 || h != 84 {
			t.Fatalf("got %dx%d, want 150x84", w, h)
		}
	})

	t.Run("custom box", func(t *testing.T) {
		w, h, err := FitSize(srcW, srcH, 800, 200)
		if err != nil {
			t.Fatal(err)
		}
		// Height-limited by 200: 356x200
		if w != 356 || h != 200 {
			t.Fatalf("got %dx%d, want 356x200", w, h)
		}
	})
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
