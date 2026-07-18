package capture_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Kevin-HYX/kvmctl/internal/capture"
)

func TestDiscoverPathsPrefersStableByIDNames(t *testing.T) {
	root := t.TempDir()
	byID := filepath.Join(root, "v4l", "by-id")
	if err := os.MkdirAll(byID, 0o755); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(byID, "usb-capture-video-index0")
	video := filepath.Join(root, "video0")
	if err := os.WriteFile(stable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := capture.DiscoverPaths(filepath.Join(byID, "*"), filepath.Join(root, "video*"))
	if err != nil {
		t.Fatalf("DiscoverPaths(): %v", err)
	}
	if len(got) != 1 || got[0] != stable {
		t.Fatalf("paths = %v, want stable path %q", got, stable)
	}
}
