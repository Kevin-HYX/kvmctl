package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kevin-HYX/kvmctl/internal/cli"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

func TestVersionReportsProgramVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{"version"}, &stdout, &stderr, cli.Dependencies{
		Version: "0.1.0-test",
	})

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), "kvmctl 0.1.0-test"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestServeRejectsNonLoopbackListenBeforeStarting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := cli.Run(context.Background(), []string{"serve", "--listen", "0.0.0.0:8080"}, &stdout, &stderr, cli.Dependencies{
		Controller:  kvm.NewMemoryController(kvm.MemoryOptions{}),
		HTTPHandler: http.NotFoundHandler(),
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("exit=%d stderr=%q, want loopback rejection", exitCode, stderr.String())
	}
}

func TestConfigValidateChecksTheExplicitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte("[keyboard]\nenabled = true\ndevice = \"/dev/hidg0\"\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr bytes.Buffer

	exitCode := cli.Run(context.Background(), []string{"config", "validate", "--file", path}, &stdout, &stderr, cli.Dependencies{})

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "configuration is valid" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestMCPHTTPRejectsNonLoopbackListenBeforeStarting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	controller := kvm.NewMemoryController(kvm.MemoryOptions{})

	exitCode := cli.Run(context.Background(), []string{"mcp", "--transport", "http", "--listen", "0.0.0.0:8080"}, &stdout, &stderr, cli.Dependencies{
		Version:    "0.1.0-test",
		Controller: controller,
	})

	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("stderr = %q, want loopback rejection", stderr.String())
	}
}

func TestMouseClickAbsoluteCLIUsesSharedController(t *testing.T) {
	var stdout, stderr bytes.Buffer
	controller := kvm.NewMemoryController(kvm.MemoryOptions{AbsolutePointer: true})

	exitCode := cli.Run(context.Background(), []string{"mouse", "click-absolute", "--x", "500", "--y", "250", "--button", "left"}, &stdout, &stderr, cli.Dependencies{Controller: controller})

	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	events := controller.Events()
	if len(events) != 2 || events[0].Kind != kvm.EventAbsoluteClick || events[0].X != 500 || events[0].Y != 250 {
		t.Fatalf("events = %#v, want absolute click and release", events)
	}
}

func TestStatusJSONReportsConfiguredCapabilitiesAndCurrentState(t *testing.T) {
	var stdout, stderr bytes.Buffer
	controller := kvm.NewMemoryController(kvm.MemoryOptions{
		ScreenConfigured:   true,
		KeyboardConfigured: true,
		AbsolutePointer:    true,
		RelativePointer:    true,
	})

	exitCode := cli.Run(context.Background(), []string{"status", "--json"}, &stdout, &stderr, cli.Dependencies{
		Version:    "0.1.0-test",
		Controller: controller,
	})

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	var got kvm.Status
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode status: %v; output = %q", err, stdout.String())
	}
	if got.Screen.State != kvm.StateReady || !got.Screen.Configured {
		t.Fatalf("screen status = %#v, want configured and ready", got.Screen)
	}
	if !got.Keyboard.Configured || !got.Pointer.Absolute.Configured || !got.Pointer.Relative.Configured {
		t.Fatalf("input capabilities not reported: %#v", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
