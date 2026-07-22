package kvm

import (
	"context"
	"image"
	"sync"
)

// State describes the current runtime state of a configured capability.
type State string

const (
	StateReady         State = "ready"
	StateNotConfigured State = "not_configured"
	StateDeviceMissing State = "device_missing"
	StateNoSignal      State = "no_signal"
	StateDisconnected  State = "disconnected"
	StateFault         State = "fault"
)

type CapabilityStatus struct {
	Configured bool   `json:"configured"`
	State      State  `json:"state"`
	Reason     string `json:"reason,omitempty"`
}

type PointerStatus struct {
	Absolute CapabilityStatus `json:"absolute"`
	Relative CapabilityStatus `json:"relative"`
}

type Status struct {
	Version  string           `json:"version,omitempty"`
	Screen   CapabilityStatus `json:"screen"`
	Keyboard CapabilityStatus `json:"keyboard"`
	Pointer  PointerStatus    `json:"pointer"`
	Control  ControlStatus    `json:"control"`
}

type ControlStatus struct {
	State     string `json:"state"`
	Holder    string `json:"holder,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type Controller interface {
	Status(context.Context) Status
}

type MemoryOptions struct {
	ScreenConfigured   bool
	KeyboardConfigured bool
	AbsolutePointer    bool
	RelativePointer    bool
	ScreenImage        image.Image
}

type MemoryController struct {
	status Status
	mu     sync.Mutex
	events []Event
	screen image.Image
}

func NewMemoryController(options MemoryOptions) *MemoryController {
	return &MemoryController{
		status: Status{
			Control:  ControlStatus{State: "idle"},
			Screen:   configuredStatus(options.ScreenConfigured),
			Keyboard: configuredStatus(options.KeyboardConfigured),
			Pointer: PointerStatus{
				Absolute: configuredStatus(options.AbsolutePointer),
				Relative: configuredStatus(options.RelativePointer),
			},
		},
		screen: options.ScreenImage,
	}
}

func (controller *MemoryController) Status(context.Context) Status {
	return controller.status
}

func configuredStatus(configured bool) CapabilityStatus {
	if configured {
		return CapabilityStatus{Configured: true, State: StateReady}
	}
	return CapabilityStatus{State: StateNotConfigured}
}
