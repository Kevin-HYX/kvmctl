package kvm

import (
	"context"
	"sync"
)

type OperationalController interface {
	Controller
	GetScreen(context.Context, ScreenRequest) (ScreenResult, error)
	KeyPress(context.Context, KeyPressRequest) error
	TypeText(context.Context, TypeTextRequest) error
	SetKeyboardState(context.Context, KeyboardStateRequest) error
	SetPointerAbsoluteState(context.Context, AbsolutePointerStateRequest) error
	SetPointerRelativeState(context.Context, RelativePointerStateRequest) error
	MouseMoveAbsolute(context.Context, AbsoluteMoveRequest) error
	MouseClickAbsolute(context.Context, AbsoluteClickRequest) error
	MouseDragAbsolute(context.Context, AbsoluteDragRequest) error
	MouseMoveRelative(context.Context, RelativeMoveRequest) error
	MouseClick(context.Context, ClickRequest) error
	MouseDragRelative(context.Context, RelativeDragRequest) error
	MouseScroll(context.Context, ScrollRequest) error
	ReleaseAll(context.Context) error
	ReleaseControl(context.Context) error
	OpenVideoStream(context.Context) (VideoStream, error)
}

type SwitchableController struct {
	mu      sync.RWMutex
	current OperationalController
}

func NewSwitchableController(controller OperationalController) *SwitchableController {
	return &SwitchableController{current: controller}
}

func (controller *SwitchableController) Swap(next OperationalController) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.current = next
}

func (controller *SwitchableController) load() OperationalController {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	return controller.current
}

func (controller *SwitchableController) Status(ctx context.Context) Status {
	return controller.load().Status(ctx)
}

func (controller *SwitchableController) GetScreen(ctx context.Context, request ScreenRequest) (ScreenResult, error) {
	return controller.load().GetScreen(ctx, request)
}

func (controller *SwitchableController) KeyPress(ctx context.Context, request KeyPressRequest) error {
	return controller.load().KeyPress(ctx, request)
}

func (controller *SwitchableController) TypeText(ctx context.Context, request TypeTextRequest) error {
	return controller.load().TypeText(ctx, request)
}

func (controller *SwitchableController) SetKeyboardState(ctx context.Context, request KeyboardStateRequest) error {
	return controller.load().SetKeyboardState(ctx, request)
}

func (controller *SwitchableController) SetPointerAbsoluteState(ctx context.Context, request AbsolutePointerStateRequest) error {
	return controller.load().SetPointerAbsoluteState(ctx, request)
}

func (controller *SwitchableController) SetPointerRelativeState(ctx context.Context, request RelativePointerStateRequest) error {
	return controller.load().SetPointerRelativeState(ctx, request)
}

func (controller *SwitchableController) MouseMoveAbsolute(ctx context.Context, request AbsoluteMoveRequest) error {
	return controller.load().MouseMoveAbsolute(ctx, request)
}

func (controller *SwitchableController) MouseClickAbsolute(ctx context.Context, request AbsoluteClickRequest) error {
	return controller.load().MouseClickAbsolute(ctx, request)
}

func (controller *SwitchableController) MouseDragAbsolute(ctx context.Context, request AbsoluteDragRequest) error {
	return controller.load().MouseDragAbsolute(ctx, request)
}

func (controller *SwitchableController) MouseMoveRelative(ctx context.Context, request RelativeMoveRequest) error {
	return controller.load().MouseMoveRelative(ctx, request)
}

func (controller *SwitchableController) MouseClick(ctx context.Context, request ClickRequest) error {
	return controller.load().MouseClick(ctx, request)
}

func (controller *SwitchableController) MouseDragRelative(ctx context.Context, request RelativeDragRequest) error {
	return controller.load().MouseDragRelative(ctx, request)
}

func (controller *SwitchableController) MouseScroll(ctx context.Context, request ScrollRequest) error {
	return controller.load().MouseScroll(ctx, request)
}

func (controller *SwitchableController) ReleaseAll(ctx context.Context) error {
	return controller.load().ReleaseAll(ctx)
}

func (controller *SwitchableController) ReleaseControl(ctx context.Context) error {
	return controller.load().ReleaseControl(ctx)
}

func (controller *SwitchableController) OpenVideoStream(ctx context.Context) (VideoStream, error) {
	return controller.load().OpenVideoStream(ctx)
}
