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
warmup_frames = 90

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

[server]
public_origin = "https://120.26.145.91:18443"
`)

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if !got.Screen.Enabled || got.Screen.CaptureTimeout != 3*time.Second || got.Screen.WarmupFrames != 90 {
		t.Fatalf("screen = %#v, want enabled with 3s timeout and 90 warmup frames", got.Screen)
	}
	if !got.Keyboard.Enabled || !got.Pointer.AbsoluteEnabled || !got.Pointer.RelativeEnabled {
		t.Fatalf("input capabilities = %#v, %#v", got.Keyboard, got.Pointer)
	}
	if got.Server.PublicOrigin != "https://120.26.145.91:18443" {
		t.Fatalf("server = %#v, want public origin", got.Server)
	}
	if !got.Server.WebRTC.Enabled || got.Server.WebRTC.ConnectTimeout != 10*time.Second {
		t.Fatalf("server WebRTC = %#v, want enabled with 10s timeout", got.Server.WebRTC)
	}
	wantSTUN := []string{"stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"}
	if len(got.Server.WebRTC.STUNURLs) != len(wantSTUN) || got.Server.WebRTC.STUNURLs[0] != wantSTUN[0] || got.Server.WebRTC.STUNURLs[1] != wantSTUN[1] {
		t.Fatalf("STUN URLs = %#v, want %#v", got.Server.WebRTC.STUNURLs, wantSTUN)
	}
}

func TestServerRejectsNonHTTPSOrPathOrigins(t *testing.T) {
	for _, origin := range []string{"http://example.com:18443", "https://example.com:18443/control", "https://user@example.com:18443"} {
		path := writeConfig(t, "[server]\npublic_origin = \""+origin+"\"\n")
		if _, err := config.Load(path); err == nil {
			t.Fatalf("Load(%q) error = nil, want invalid public origin", origin)
		}
	}
}

func TestWebRTCConfigurationCanDisableDefaultSTUN(t *testing.T) {
	path := writeConfig(t, `
[server]
public_origin = "https://example.com:18443"

[server.webrtc]
enabled = false
stun_urls = []
connect_timeout = "5s"
`)
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server.WebRTC.Enabled || len(got.Server.WebRTC.STUNURLs) != 0 || got.Server.WebRTC.ConnectTimeout != 5*time.Second {
		t.Fatalf("WebRTC = %#v, want disabled without STUN and 5s timeout", got.Server.WebRTC)
	}
}

func TestWebRTCConfigurationRejectsTURNOrCredentialedURLs(t *testing.T) {
	for _, candidate := range []string{"turn:example.com:3478", "stun:user@example.com:3478", "stun:example.com:3478/path"} {
		path := writeConfig(t, "[server.webrtc]\nstun_urls = [\""+candidate+"\"]\n")
		if _, err := config.Load(path); err == nil {
			t.Fatalf("Load(%q) error = nil, want STUN-only URL validation", candidate)
		}
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
	if !got.Screen.Enabled || got.Screen.Device != "" || got.Screen.WarmupFrames != 90 {
		t.Fatalf("screen = %#v, want enabled automatic discovery with default warmup", got.Screen)
	}
}

func TestScreenWarmupCanBeDisabled(t *testing.T) {
	path := writeConfig(t, "[screen]\nenabled = true\nwarmup_frames = 0\n")
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if got.Screen.WarmupFrames != 0 {
		t.Fatalf("warmup frames = %d, want 0", got.Screen.WarmupFrames)
	}
}

func TestScreenRejectsExcessiveWarmupFrames(t *testing.T) {
	path := writeConfig(t, "[screen]\nenabled = true\nwarmup_frames = 601\n")
	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want warmup frame validation error")
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
