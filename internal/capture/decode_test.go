package capture_test

import (
	"image/color"
	"testing"

	"github.com/Kevin-HYX/kvmctl/internal/capture"
)

func TestDecodeYUYVProducesTwoPixels(t *testing.T) {
	// Neutral chroma: first luma is black, second is white.
	frame, err := capture.DecodeYUYV([]byte{16, 128, 235, 128}, 2, 1)
	if err != nil {
		t.Fatalf("DecodeYUYV(): %v", err)
	}
	black := color.RGBAModel.Convert(frame.At(0, 0)).(color.RGBA)
	white := color.RGBAModel.Convert(frame.At(1, 0)).(color.RGBA)
	if black.R > 5 || black.G > 5 || black.B > 5 {
		t.Fatalf("first pixel = %#v, want black", black)
	}
	if white.R < 245 || white.G < 245 || white.B < 245 {
		t.Fatalf("second pixel = %#v, want white", white)
	}
}

func TestDecodeYUYVRejectsTruncatedFrame(t *testing.T) {
	if _, err := capture.DecodeYUYV([]byte{16, 128}, 2, 1); err == nil {
		t.Fatal("DecodeYUYV() error = nil, want truncated frame rejection")
	}
}
