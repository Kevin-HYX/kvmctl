package kvm

import "context"

type KeyboardStateRequest struct {
	Keys []string `json:"keys"`
}

type AbsolutePointerStateRequest struct {
	X       int      `json:"x"`
	Y       int      `json:"y"`
	Buttons []string `json:"buttons"`
	Wheel   int      `json:"wheel,omitempty"`
}

type RelativePointerStateRequest struct {
	DX      int      `json:"dx"`
	DY      int      `json:"dy"`
	Buttons []string `json:"buttons"`
	Wheel   int      `json:"wheel,omitempty"`
}

type BrowserInputController interface {
	SetKeyboardState(context.Context, KeyboardStateRequest) error
	SetPointerAbsoluteState(context.Context, AbsolutePointerStateRequest) error
	SetPointerRelativeState(context.Context, RelativePointerStateRequest) error
	ReleaseAll(context.Context) error
}

// ValidateKeyboardStateRequest 校验浏览器键盘状态，但不执行输入。
func ValidateKeyboardStateRequest(request KeyboardStateRequest) error {
	return validateKeyboardState(request)
}

// ValidateAbsolutePointerStateRequest 校验浏览器绝对指针状态，但不执行输入。
func ValidateAbsolutePointerStateRequest(request AbsolutePointerStateRequest) error {
	return validateAbsolutePointerState(request)
}

// ValidateRelativePointerStateRequest 校验浏览器相对指针状态，但不执行输入。
func ValidateRelativePointerStateRequest(request RelativePointerStateRequest) error {
	return validateRelativePointerState(request)
}

func validateKeyboardState(request KeyboardStateRequest) error {
	if len(request.Keys) == 0 {
		return nil
	}
	return validateKeyPress(KeyPressRequest{Keys: request.Keys})
}

func validateAbsolutePointerState(request AbsolutePointerStateRequest) error {
	if err := validateCoordinates(request.X, request.Y); err != nil {
		return err
	}
	return validatePointerState(request.Buttons, request.Wheel)
}

func validateRelativePointerState(request RelativePointerStateRequest) error {
	if request.DX < -32767 || request.DX > 32767 || request.DY < -32767 || request.DY > 32767 {
		return invalidArgument("dx and dy must be between -32767 and 32767")
	}
	return validatePointerState(request.Buttons, request.Wheel)
}

func validatePointerState(buttons []string, wheel int) error {
	if len(buttons) > 3 || wheel < -100 || wheel > 100 {
		return invalidArgument("buttons or wheel is invalid")
	}
	seen := make(map[string]struct{}, len(buttons))
	for _, button := range buttons {
		if !validButton(button) {
			return invalidArgument("unsupported pointer button")
		}
		if _, exists := seen[button]; exists {
			return invalidArgument("pointer buttons must be unique")
		}
		seen[button] = struct{}{}
	}
	return nil
}
