package app

import (
	"fmt"

	"github.com/Kevin-HYX/kvmctl/internal/config"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

func ReloadController(path string, previous config.Config, controller *kvm.SwitchableController) (config.Config, error) {
	candidate, err := config.Load(path)
	if err != nil {
		return previous, err
	}
	if capabilityShape(candidate) != capabilityShape(previous) {
		return previous, fmt.Errorf("changing configured capabilities requires a service restart")
	}
	if candidate.MCP.ControlLease != previous.MCP.ControlLease {
		return previous, fmt.Errorf("changing mcp.control_lease requires a service restart")
	}
	controller.Swap(BuildController(candidate))
	return candidate, nil
}

type capabilities struct {
	screen, keyboard, absolute, relative bool
}

func capabilityShape(configuration config.Config) capabilities {
	return capabilities{
		screen:   configuration.Screen.Enabled,
		keyboard: configuration.Keyboard.Enabled,
		absolute: configuration.Pointer.AbsoluteEnabled,
		relative: configuration.Pointer.RelativeEnabled,
	}
}
