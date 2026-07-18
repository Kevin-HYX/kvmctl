package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/config"
)

func TestLoadValidatesExplicitHardwareConfiguration(t *testing.T) {
	path := writeConfig(t, `
[screen]
enabled = true
device = "/dev/v4l/by-id/usb-capture"
capture_timeout = "3s"

[keyboard]
enabled = true
device = "/dev/hidg0"

[pointer]
absolute_enabled = true
absolute_device = "/dev/hidg1"
relative_enabled = true
relative_device = "/dev/hidg2"

[mcp]
control_lease = "30s"
`)

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !got.Screen.Enabled || got.Screen.CaptureTimeout != 3*time.Second {
		t.Fatalf("screen = %#v, want enabled with 3s timeout", got.Screen)
	}
	if !got.Keyboard.Enabled || !got.Pointer.AbsoluteEnabled || !got.Pointer.RelativeEnabled {
		t.Fatalf("input capabilities = %#v, %#v", got.Keyboard, got.Pointer)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, "[screen]\nenabled = true\nmagic_fallback = true\n")

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want unknown field rejection")
	}
}

func TestScreenCanUseAutomaticDiscovery(t *testing.T) {
	path := writeConfig(t, "[screen]\nenabled = true\n")
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !got.Screen.Enabled || got.Screen.Device != "" {
		t.Fatalf("screen = %#v, want enabled automatic discovery", got.Screen)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
