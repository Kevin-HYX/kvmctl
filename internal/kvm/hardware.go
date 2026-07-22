package kvm

import (
	"context"
	"errors"
	"image"
	"time"
)

type KeyboardDevice interface {
	Press([]string, time.Duration) error
	Type(string, time.Duration) error
	Set([]string) error
	Release() error
}

type AbsolutePointerDevice interface {
	Set(x, y int, buttons []string, wheel int) error
	Move(x, y int) error
	Click(x, y int, button string, clicks int, interval time.Duration) error
	Drag(fromX, fromY, toX, toY int, button string, duration time.Duration) error
	Release() error
}

type RelativePointerDevice interface {
	Set(dx, dy int, buttons []string, wheel int) error
	Move(dx, dy int) error
	Click(button string, clicks int, interval time.Duration) error
	Drag(dx, dy int, button string, duration time.Duration) error
	Scroll(delta int) error
	Release() error
}

type ScreenSource interface {
	Capture(context.Context) (image.Image, error)
}

type HardwareOptions struct {
	Screen               ScreenSource
	Keyboard             KeyboardDevice
	AbsolutePointer      AbsolutePointerDevice
	RelativePointer      RelativePointerDevice
	ScreenProbe          func() error
	KeyboardProbe        func() error
	AbsolutePointerProbe func() error
	RelativePointerProbe func() error
}

type HardwareController struct {
	screen               ScreenSource
	keyboard             KeyboardDevice
	absolutePointer      AbsolutePointerDevice
	relativePointer      RelativePointerDevice
	screenProbe          func() error
	keyboardProbe        func() error
	absolutePointerProbe func() error
	relativePointerProbe func() error
}

func NewHardwareController(options HardwareOptions) *HardwareController {
	return &HardwareController{
		screen:               options.Screen,
		keyboard:             options.Keyboard,
		absolutePointer:      options.AbsolutePointer,
		relativePointer:      options.RelativePointer,
		screenProbe:          options.ScreenProbe,
		keyboardProbe:        options.KeyboardProbe,
		absolutePointerProbe: options.AbsolutePointerProbe,
		relativePointerProbe: options.RelativePointerProbe,
	}
}

func (controller *HardwareController) Status(context.Context) Status {
	return Status{
		Control:  ControlStatus{State: "idle"},
		Screen:   probedStatus(controller.screen != nil, controller.screenProbe),
		Keyboard: probedStatus(controller.keyboard != nil, controller.keyboardProbe),
		Pointer: PointerStatus{
			Absolute: probedStatus(controller.absolutePointer != nil, controller.absolutePointerProbe),
			Relative: probedStatus(controller.relativePointer != nil, controller.relativePointerProbe),
		},
	}
}

func probedStatus(configured bool, probe func() error) CapabilityStatus {
	if !configured {
		return configuredStatus(false)
	}
	if probe != nil && probe() != nil {
		return CapabilityStatus{Configured: true, State: StateDeviceMissing, Reason: "configured device is unavailable"}
	}
	return configuredStatus(true)
}

func (controller *HardwareController) GetScreen(ctx context.Context, request ScreenRequest) (ScreenResult, error) {
	if controller.screen == nil {
		return ScreenResult{}, capabilityNotConfigured("screen")
	}
	frame, err := controller.screen.Capture(ctx)
	if err != nil {
		var operationError *OperationError
		if errors.As(err, &operationError) {
			return ScreenResult{}, operationError
		}
		return ScreenResult{}, &OperationError{Code: ErrorInternal, Message: "screen capture failed", Recovery: "inspect capture diagnostics and retry"}
	}
	if frame == nil {
		return ScreenResult{}, &OperationError{Code: ErrorNoVideoSignal, Message: "the screen source has no current frame", Recovery: "check the HDMI source and capture device"}
	}
	return renderScreen(frame, request)
}

func (controller *HardwareController) KeyPress(_ context.Context, request KeyPressRequest) error {
	if controller.keyboard == nil {
		return capabilityNotConfigured("keyboard")
	}
	if err := validateKeyPress(request); err != nil {
		return err
	}
	if request.HoldMS == 0 {
		request.HoldMS = 80
	}
	return wrapInputError(controller.keyboard.Press(request.Keys, time.Duration(request.HoldMS)*time.Millisecond))
}

func (controller *HardwareController) TypeText(_ context.Context, request TypeTextRequest) error {
	if controller.keyboard == nil {
		return capabilityNotConfigured("keyboard")
	}
	if err := validateTypeText(request); err != nil {
		return err
	}
	if request.IntervalMS == 0 {
		request.IntervalMS = 20
	}
	return wrapInputError(controller.keyboard.Type(request.Text, time.Duration(request.IntervalMS)*time.Millisecond))
}

func (controller *HardwareController) SetKeyboardState(_ context.Context, request KeyboardStateRequest) error {
	if controller.keyboard == nil {
		return capabilityNotConfigured("keyboard")
	}
	if err := validateKeyboardState(request); err != nil {
		return err
	}
	return wrapInputError(controller.keyboard.Set(request.Keys))
}

func (controller *HardwareController) SetPointerAbsoluteState(_ context.Context, request AbsolutePointerStateRequest) error {
	if controller.absolutePointer == nil {
		return capabilityNotConfigured("absolute pointer")
	}
	if err := validateAbsolutePointerState(request); err != nil {
		return err
	}
	return wrapInputError(controller.absolutePointer.Set(request.X, request.Y, request.Buttons, request.Wheel))
}

func (controller *HardwareController) SetPointerRelativeState(_ context.Context, request RelativePointerStateRequest) error {
	if controller.relativePointer == nil {
		return capabilityNotConfigured("relative pointer")
	}
	if err := validateRelativePointerState(request); err != nil {
		return err
	}
	return wrapInputError(controller.relativePointer.Set(request.DX, request.DY, request.Buttons, request.Wheel))
}

func (controller *HardwareController) MouseMoveAbsolute(_ context.Context, request AbsoluteMoveRequest) error {
	if controller.absolutePointer == nil {
		return capabilityNotConfigured("absolute pointer")
	}
	if err := validateCoordinates(request.X, request.Y); err != nil {
		return err
	}
	return wrapInputError(controller.absolutePointer.Move(request.X, request.Y))
}

func (controller *HardwareController) MouseClickAbsolute(_ context.Context, request AbsoluteClickRequest) error {
	if controller.absolutePointer == nil {
		return capabilityNotConfigured("absolute pointer")
	}
	if err := validateCoordinates(request.X, request.Y); err != nil {
		return err
	}
	applyClickDefaults(&request.Clicks, &request.IntervalMS)
	if err := validateClick(request.Button, request.Clicks, request.IntervalMS); err != nil {
		return err
	}
	return wrapInputError(controller.absolutePointer.Click(request.X, request.Y, request.Button, request.Clicks, time.Duration(request.IntervalMS)*time.Millisecond))
}

func (controller *HardwareController) MouseDragAbsolute(_ context.Context, request AbsoluteDragRequest) error {
	if controller.absolutePointer == nil {
		return capabilityNotConfigured("absolute pointer")
	}
	if err := validateCoordinates(request.FromX, request.FromY); err != nil {
		return err
	}
	if err := validateCoordinates(request.ToX, request.ToY); err != nil {
		return err
	}
	if request.DurationMS == 0 {
		request.DurationMS = 500
	}
	if !validButton(request.Button) || request.DurationMS < 50 || request.DurationMS > 5000 {
		return invalidArgument("button or duration_ms is invalid")
	}
	return wrapInputError(controller.absolutePointer.Drag(request.FromX, request.FromY, request.ToX, request.ToY, request.Button, time.Duration(request.DurationMS)*time.Millisecond))
}

func (controller *HardwareController) MouseMoveRelative(_ context.Context, request RelativeMoveRequest) error {
	if controller.relativePointer == nil {
		return capabilityNotConfigured("relative pointer")
	}
	if request.DX == 0 && request.DY == 0 {
		return invalidArgument("dx and dy cannot both be zero")
	}
	if request.DX < -32767 || request.DX > 32767 || request.DY < -32767 || request.DY > 32767 {
		return invalidArgument("dx and dy must be between -32767 and 32767")
	}
	return wrapInputError(controller.relativePointer.Move(request.DX, request.DY))
}

func (controller *HardwareController) MouseClick(_ context.Context, request ClickRequest) error {
	if controller.relativePointer == nil {
		return capabilityNotConfigured("relative pointer")
	}
	applyClickDefaults(&request.Clicks, &request.IntervalMS)
	if err := validateClick(request.Button, request.Clicks, request.IntervalMS); err != nil {
		return err
	}
	return wrapInputError(controller.relativePointer.Click(request.Button, request.Clicks, time.Duration(request.IntervalMS)*time.Millisecond))
}

func (controller *HardwareController) MouseDragRelative(_ context.Context, request RelativeDragRequest) error {
	if controller.relativePointer == nil {
		return capabilityNotConfigured("relative pointer")
	}
	if request.DX == 0 && request.DY == 0 {
		return invalidArgument("dx and dy cannot both be zero")
	}
	if request.DX < -32767 || request.DX > 32767 || request.DY < -32767 || request.DY > 32767 {
		return invalidArgument("dx and dy must be between -32767 and 32767")
	}
	if request.DurationMS == 0 {
		request.DurationMS = 500
	}
	if !validButton(request.Button) || request.DurationMS < 50 || request.DurationMS > 5000 {
		return invalidArgument("button or duration_ms is invalid")
	}
	return wrapInputError(controller.relativePointer.Drag(request.DX, request.DY, request.Button, time.Duration(request.DurationMS)*time.Millisecond))
}

func (controller *HardwareController) MouseScroll(_ context.Context, request ScrollRequest) error {
	if controller.relativePointer == nil {
		return capabilityNotConfigured("relative pointer")
	}
	if request.Delta < -100 || request.Delta > 100 || request.Delta == 0 {
		return invalidArgument("delta must be between -100 and 100 and cannot be zero")
	}
	return wrapInputError(controller.relativePointer.Scroll(request.Delta))
}

func (controller *HardwareController) ReleaseAll(context.Context) error {
	var releaseErrors []error
	if controller.keyboard != nil {
		releaseErrors = appendIfError(releaseErrors, controller.keyboard.Release())
	}
	if controller.absolutePointer != nil {
		releaseErrors = appendIfError(releaseErrors, controller.absolutePointer.Release())
	}
	if controller.relativePointer != nil {
		releaseErrors = appendIfError(releaseErrors, controller.relativePointer.Release())
	}
	return wrapInputError(errors.Join(releaseErrors...))
}

func (controller *HardwareController) ReleaseControl(ctx context.Context) error {
	return controller.ReleaseAll(ctx)
}

func appendIfError(existing []error, err error) []error {
	if err != nil {
		return append(existing, err)
	}
	return existing
}

func wrapInputError(err error) error {
	if err == nil {
		return nil
	}
	return &OperationError{Code: ErrorInputFailed, Message: "HID report write failed", Recovery: "check the USB connection, HID device node, and service permissions"}
}
