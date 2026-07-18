package kvm

import (
	"context"
	"fmt"
)

type ErrorCode string

const (
	ErrorInvalidArgument         ErrorCode = "INVALID_ARGUMENT"
	ErrorCapabilityNotConfigured ErrorCode = "CAPABILITY_NOT_CONFIGURED"
	ErrorDeviceMissing           ErrorCode = "DEVICE_MISSING"
	ErrorNoVideoSignal           ErrorCode = "NO_VIDEO_SIGNAL"
	ErrorCaptureTimeout          ErrorCode = "CAPTURE_TIMEOUT"
	ErrorHostDisconnected        ErrorCode = "HOST_DISCONNECTED"
	ErrorControlBusy             ErrorCode = "CONTROL_BUSY"
	ErrorInputFailed             ErrorCode = "INPUT_FAILED"
	ErrorOperationTimeout        ErrorCode = "OPERATION_TIMEOUT"
	ErrorInternal                ErrorCode = "INTERNAL_ERROR"
)

type OperationError struct {
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	Recovery string    `json:"recovery"`
}

func (err *OperationError) Error() string {
	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}

type KeyPressRequest struct {
	Keys   []string `json:"keys" jsonschema:"One to six canonical non-modifier key names, optionally with ctrl, shift, alt, or meta."`
	HoldMS int      `json:"hold_ms,omitempty" jsonschema:"Hold duration in milliseconds, 20 through 5000; defaults to 80 when omitted."`
}

type TypeTextRequest struct {
	Text       string `json:"text" jsonschema:"One to 1024 printable ASCII characters typed with a US keyboard layout; use key_press for control keys."`
	IntervalMS int    `json:"interval_ms,omitempty" jsonschema:"Delay between characters in milliseconds, 0 through 500; defaults to 20 when omitted."`
}

type RelativeMoveRequest struct {
	DX int `json:"dx" jsonschema:"Horizontal signed HID counts from -32767 through 32767."`
	DY int `json:"dy" jsonschema:"Vertical signed HID counts from -32767 through 32767; dx and dy cannot both be zero."`
}

type AbsoluteMoveRequest struct {
	X int `json:"x" jsonschema:"Normalized horizontal coordinate from 0 at the left to 1000 at the right."`
	Y int `json:"y" jsonschema:"Normalized vertical coordinate from 0 at the top to 1000 at the bottom."`
}

type AbsoluteClickRequest struct {
	X          int    `json:"x" jsonschema:"Normalized horizontal coordinate from 0 through 1000."`
	Y          int    `json:"y" jsonschema:"Normalized vertical coordinate from 0 through 1000."`
	Button     string `json:"button" jsonschema:"Pointer button: left, right, or middle."`
	Clicks     int    `json:"clicks,omitempty" jsonschema:"Number of clicks, 1 through 3; defaults to 1."`
	IntervalMS int    `json:"interval_ms,omitempty" jsonschema:"Delay between multiple clicks in milliseconds, 50 through 1000; defaults to 120."`
}

type AbsoluteDragRequest struct {
	FromX      int    `json:"from_x" jsonschema:"Normalized starting x coordinate, 0 through 1000."`
	FromY      int    `json:"from_y" jsonschema:"Normalized starting y coordinate, 0 through 1000."`
	ToX        int    `json:"to_x" jsonschema:"Normalized ending x coordinate, 0 through 1000."`
	ToY        int    `json:"to_y" jsonschema:"Normalized ending y coordinate, 0 through 1000."`
	Button     string `json:"button" jsonschema:"Pointer button: left, right, or middle."`
	DurationMS int    `json:"duration_ms,omitempty" jsonschema:"Drag duration in milliseconds, 50 through 5000; defaults to 500."`
}

type ClickRequest struct {
	Button     string `json:"button" jsonschema:"Pointer button: left, right, or middle."`
	Clicks     int    `json:"clicks,omitempty" jsonschema:"Number of clicks, 1 through 3; defaults to 1."`
	IntervalMS int    `json:"interval_ms,omitempty" jsonschema:"Delay between multiple clicks in milliseconds, 50 through 1000; defaults to 120."`
}

type RelativeDragRequest struct {
	DX         int    `json:"dx" jsonschema:"Horizontal signed HID counts from -32767 through 32767."`
	DY         int    `json:"dy" jsonschema:"Vertical signed HID counts from -32767 through 32767; dx and dy cannot both be zero."`
	Button     string `json:"button" jsonschema:"Pointer button: left, right, or middle."`
	DurationMS int    `json:"duration_ms,omitempty" jsonschema:"Drag duration in milliseconds, 50 through 5000; defaults to 500."`
}

type ScrollRequest struct {
	Delta int `json:"delta" jsonschema:"Vertical scroll amount from -100 through 100 excluding zero; positive scrolls up."`
}

type EventKind string

const (
	EventKeyPress       EventKind = "key_press"
	EventTypeText       EventKind = "type_text"
	EventAbsoluteMove   EventKind = "absolute_move"
	EventAbsoluteClick  EventKind = "absolute_click"
	EventAbsoluteDrag   EventKind = "absolute_drag"
	EventRelativeMove   EventKind = "relative_move"
	EventClick          EventKind = "click"
	EventRelativeDrag   EventKind = "relative_drag"
	EventScroll         EventKind = "scroll"
	EventReleaseAll     EventKind = "release_all"
	EventReleaseControl EventKind = "release_control"
)

type Event struct {
	Kind   EventKind
	Keys   []string
	Text   string
	DX     int
	DY     int
	X      int
	Y      int
	ToX    int
	ToY    int
	Button string
	Clicks int
	Delta  int
}

func (controller *MemoryController) KeyPress(_ context.Context, request KeyPressRequest) error {
	if !controller.status.Keyboard.Configured {
		return capabilityNotConfigured("keyboard")
	}
	if err := validateKeyPress(request); err != nil {
		return err
	}
	controller.appendEvents(
		Event{Kind: EventKeyPress, Keys: append([]string(nil), request.Keys...)},
		Event{Kind: EventReleaseAll},
	)
	return nil
}

func (controller *MemoryController) TypeText(_ context.Context, request TypeTextRequest) error {
	if !controller.status.Keyboard.Configured {
		return capabilityNotConfigured("keyboard")
	}
	if err := validateTypeText(request); err != nil {
		return err
	}
	controller.appendEvents(Event{Kind: EventTypeText, Text: request.Text}, Event{Kind: EventReleaseAll})
	return nil
}

func (controller *MemoryController) MouseMoveRelative(_ context.Context, request RelativeMoveRequest) error {
	if !controller.status.Pointer.Relative.Configured {
		return capabilityNotConfigured("relative pointer")
	}
	if request.DX == 0 && request.DY == 0 {
		return invalidArgument("dx and dy cannot both be zero")
	}
	if request.DX < -32767 || request.DX > 32767 || request.DY < -32767 || request.DY > 32767 {
		return invalidArgument("dx and dy must be between -32767 and 32767")
	}

	remainingX, remainingY := request.DX, request.DY
	for remainingX != 0 || remainingY != 0 {
		dx := clamp(remainingX, -127, 127)
		dy := clamp(remainingY, -127, 127)
		controller.appendEvents(Event{Kind: EventRelativeMove, DX: dx, DY: dy})
		remainingX -= dx
		remainingY -= dy
	}
	return nil
}

func (controller *MemoryController) MouseMoveAbsolute(_ context.Context, request AbsoluteMoveRequest) error {
	if !controller.status.Pointer.Absolute.Configured {
		return capabilityNotConfigured("absolute pointer")
	}
	if err := validateCoordinates(request.X, request.Y); err != nil {
		return err
	}
	controller.appendEvents(Event{Kind: EventAbsoluteMove, X: request.X, Y: request.Y})
	return nil
}

func (controller *MemoryController) MouseClickAbsolute(_ context.Context, request AbsoluteClickRequest) error {
	if !controller.status.Pointer.Absolute.Configured {
		return capabilityNotConfigured("absolute pointer")
	}
	if err := validateCoordinates(request.X, request.Y); err != nil {
		return err
	}
	applyClickDefaults(&request.Clicks, &request.IntervalMS)
	if err := validateClick(request.Button, request.Clicks, request.IntervalMS); err != nil {
		return err
	}
	controller.appendEvents(
		Event{Kind: EventAbsoluteClick, X: request.X, Y: request.Y, Button: request.Button, Clicks: request.Clicks},
		Event{Kind: EventReleaseAll},
	)
	return nil
}

func (controller *MemoryController) MouseDragAbsolute(_ context.Context, request AbsoluteDragRequest) error {
	if !controller.status.Pointer.Absolute.Configured {
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
	controller.appendEvents(
		Event{Kind: EventAbsoluteDrag, X: request.FromX, Y: request.FromY, ToX: request.ToX, ToY: request.ToY, Button: request.Button},
		Event{Kind: EventReleaseAll},
	)
	return nil
}

func (controller *MemoryController) MouseClick(_ context.Context, request ClickRequest) error {
	if !controller.status.Pointer.Relative.Configured {
		return capabilityNotConfigured("relative pointer")
	}
	applyClickDefaults(&request.Clicks, &request.IntervalMS)
	if err := validateClick(request.Button, request.Clicks, request.IntervalMS); err != nil {
		return err
	}
	controller.appendEvents(Event{Kind: EventClick, Button: request.Button, Clicks: request.Clicks}, Event{Kind: EventReleaseAll})
	return nil
}

func (controller *MemoryController) MouseDragRelative(_ context.Context, request RelativeDragRequest) error {
	if !controller.status.Pointer.Relative.Configured {
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
	controller.appendEvents(Event{Kind: EventRelativeDrag, DX: request.DX, DY: request.DY, Button: request.Button}, Event{Kind: EventReleaseAll})
	return nil
}

func (controller *MemoryController) MouseScroll(_ context.Context, request ScrollRequest) error {
	if !controller.status.Pointer.Relative.Configured {
		return capabilityNotConfigured("relative pointer")
	}
	if request.Delta < -100 || request.Delta > 100 || request.Delta == 0 {
		return invalidArgument("delta must be between -100 and 100 and cannot be zero")
	}
	controller.appendEvents(Event{Kind: EventScroll, Delta: request.Delta})
	return nil
}

func (controller *MemoryController) ReleaseAll(context.Context) error {
	controller.appendEvents(Event{Kind: EventReleaseAll})
	return nil
}

func (controller *MemoryController) ReleaseControl(context.Context) error {
	controller.appendEvents(Event{Kind: EventReleaseAll}, Event{Kind: EventReleaseControl})
	return nil
}

func (controller *MemoryController) Events() []Event {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return append([]Event(nil), controller.events...)
}

func (controller *MemoryController) appendEvents(events ...Event) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.events = append(controller.events, events...)
}

func invalidArgument(message string) error {
	return &OperationError{Code: ErrorInvalidArgument, Message: message, Recovery: "correct the arguments and retry"}
}

func capabilityNotConfigured(capability string) error {
	return &OperationError{
		Code:     ErrorCapabilityNotConfigured,
		Message:  capability + " is not configured",
		Recovery: "enable the capability in the kvmctl configuration",
	}
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func validateCoordinates(x, y int) error {
	if x < 0 || x > 1000 || y < 0 || y > 1000 {
		return invalidArgument("coordinates must be between 0 and 1000")
	}
	return nil
}

func applyClickDefaults(clicks, intervalMS *int) {
	if *clicks == 0 {
		*clicks = 1
	}
	if *intervalMS == 0 {
		*intervalMS = 120
	}
}

func validateClick(button string, clicks, intervalMS int) error {
	if !validButton(button) || clicks < 1 || clicks > 3 || intervalMS < 50 || intervalMS > 1000 {
		return invalidArgument("button, clicks, or interval_ms is invalid")
	}
	return nil
}

func validButton(button string) bool {
	return button == "left" || button == "right" || button == "middle"
}

func validateKeyPress(request KeyPressRequest) error {
	if len(request.Keys) == 0 || len(request.Keys) > 10 {
		return invalidArgument("keys must contain at least one key")
	}
	nonModifiers := 0
	for _, key := range request.Keys {
		if key != "ctrl" && key != "shift" && key != "alt" && key != "meta" {
			nonModifiers++
			if _, ok := supportedKeyNames[key]; !ok {
				return invalidArgument("unsupported key name")
			}
		}
	}
	if nonModifiers > 6 {
		return invalidArgument("at most six non-modifier keys are supported")
	}
	if request.HoldMS != 0 && (request.HoldMS < 20 || request.HoldMS > 5000) {
		return invalidArgument("hold_ms must be between 20 and 5000")
	}
	return nil
}

var supportedKeyNames = buildSupportedKeyNames()

func buildSupportedKeyNames() map[string]struct{} {
	names := map[string]struct{}{
		"enter": {}, "escape": {}, "backspace": {}, "tab": {}, "space": {}, "minus": {}, "equal": {},
		"leftbracket": {}, "rightbracket": {}, "backslash": {}, "semicolon": {}, "apostrophe": {},
		"grave": {}, "comma": {}, "period": {}, "slash": {}, "capslock": {}, "printscreen": {},
		"scrolllock": {}, "pause": {}, "insert": {}, "home": {}, "pageup": {}, "delete": {},
		"end": {}, "pagedown": {}, "right": {}, "left": {}, "down": {}, "up": {}, "numlock": {},
	}
	for character := 'a'; character <= 'z'; character++ {
		names[string(character)] = struct{}{}
	}
	for character := '0'; character <= '9'; character++ {
		names[string(character)] = struct{}{}
	}
	for index := 1; index <= 12; index++ {
		names[fmt.Sprintf("f%d", index)] = struct{}{}
	}
	return names
}

func validateTypeText(request TypeTextRequest) error {
	if len(request.Text) == 0 || len(request.Text) > 1024 {
		return invalidArgument("text must contain between 1 and 1024 bytes")
	}
	for _, character := range []byte(request.Text) {
		if character < 0x20 || character > 0x7e {
			return invalidArgument("text must contain printable ASCII only")
		}
	}
	if request.IntervalMS < 0 || request.IntervalMS > 500 {
		return invalidArgument("interval_ms must be between 0 and 500")
	}
	return nil
}
