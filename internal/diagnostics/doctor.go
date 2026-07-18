package diagnostics

import (
	"context"
	"os"
	"runtime"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

type Check struct {
	Name     string    `json:"name"`
	State    kvm.State `json:"state"`
	Detail   string    `json:"detail"`
	Recovery string    `json:"recovery,omitempty"`
}

type Report struct {
	Healthy bool    `json:"healthy"`
	Checks  []Check `json:"checks"`
}

func Doctor(ctx context.Context, controller kvm.Controller) Report {
	report := Report{Healthy: true}
	if runtime.GOOS == "linux" {
		report.Checks = append(report.Checks, Check{Name: "runtime", State: kvm.StateReady, Detail: "Linux runtime"})
	} else {
		addFailure(&report, Check{Name: "runtime", State: kvm.StateFault, Detail: "hardware adapters require Linux", Recovery: "run kvmctl on the Linux control device"})
	}
	udcs, err := os.ReadDir("/sys/class/udc")
	if err != nil || len(udcs) == 0 {
		addFailure(&report, Check{Name: "usb_device_controller", State: kvm.StateDeviceMissing, Detail: "no USB Device Controller is available", Recovery: "load the board OTG/UDC driver and verify device mode"})
	} else {
		report.Checks = append(report.Checks, Check{Name: "usb_device_controller", State: kvm.StateReady, Detail: "USB Device Controller is available"})
	}
	if controller == nil {
		addFailure(&report, Check{Name: "controller", State: kvm.StateFault, Detail: "controller is not configured"})
		return report
	}
	status := controller.Status(ctx)
	appendCapability(&report, "screen", status.Screen)
	appendCapability(&report, "keyboard", status.Keyboard)
	appendCapability(&report, "pointer_absolute", status.Pointer.Absolute)
	appendCapability(&report, "pointer_relative", status.Pointer.Relative)
	return report
}

func appendCapability(report *Report, name string, status kvm.CapabilityStatus) {
	if !status.Configured {
		report.Checks = append(report.Checks, Check{Name: name, State: status.State, Detail: "capability is not configured"})
		return
	}
	check := Check{Name: name, State: status.State, Detail: status.Reason}
	if status.State != kvm.StateReady {
		check.Recovery = "check the configured device node and service permissions"
		addFailure(report, check)
		return
	}
	check.Detail = "configured device is available"
	report.Checks = append(report.Checks, check)
}

func addFailure(report *Report, check Check) {
	report.Healthy = false
	report.Checks = append(report.Checks, check)
}
