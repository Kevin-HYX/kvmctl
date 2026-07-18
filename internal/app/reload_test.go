package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kevin-HYX/kvmctl/internal/app"
	"github.com/Kevin-HYX/kvmctl/internal/config"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

func TestInvalidReloadKeepsPreviousController(t *testing.T) {
	previous := config.Config{Keyboard: config.Keyboard{Enabled: true, Device: "/dev/hidg0"}, MCP: config.MCP{ControlLease: 30_000_000_000}}
	controller := kvm.NewSwitchableController(app.BuildController(previous))
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("unknown = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	retained, err := app.ReloadController(path, previous, controller)
	if err == nil {
		t.Fatal("ReloadController() error = nil, want invalid configuration error")
	}
	if !retained.Keyboard.Enabled || !controller.Status(context.Background()).Keyboard.Configured {
		t.Fatalf("invalid reload replaced prior configuration: %#v", retained)
	}
}
