package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type emptyArguments struct{}

type toolResult struct {
	OK bool `json:"ok"`
}

type keyPresser interface {
	KeyPress(context.Context, kvm.KeyPressRequest) error
}

type textTyper interface {
	TypeText(context.Context, kvm.TypeTextRequest) error
}

type pointerController interface {
	MouseMoveAbsolute(context.Context, kvm.AbsoluteMoveRequest) error
	MouseClickAbsolute(context.Context, kvm.AbsoluteClickRequest) error
	MouseDragAbsolute(context.Context, kvm.AbsoluteDragRequest) error
	MouseMoveRelative(context.Context, kvm.RelativeMoveRequest) error
	MouseClick(context.Context, kvm.ClickRequest) error
	MouseDragRelative(context.Context, kvm.RelativeDragRequest) error
	MouseScroll(context.Context, kvm.ScrollRequest) error
	ReleaseAll(context.Context) error
	ReleaseControl(context.Context) error
}

type screenController interface {
	GetScreen(context.Context, kvm.ScreenRequest) (kvm.ScreenResult, error)
}

type Options struct {
	ControlLease time.Duration
}

func toolDefinition(name, description string, readOnly, idempotent bool) *mcp.Tool {
	closedWorld := false
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: idempotent, OpenWorldHint: &closedWorld},
	}
}

func New(version string, controller kvm.Controller) *mcp.Server {
	return NewWithOptions(version, controller, Options{ControlLease: 30 * time.Second})
}

func NewWithOptions(version string, controller kvm.Controller, options Options) *mcp.Server {
	if options.ControlLease <= 0 {
		options.ControlLease = 30 * time.Second
	}
	control := newControlManager(options.ControlLease, releaseControllerInput(controller))
	server := mcp.NewServer(
		&mcp.Implementation{Name: "kvmctl", Version: version},
		&mcp.ServerOptions{Instructions: "Observe the current screen when needed, then use atomic keyboard and pointer tools. All absolute coordinates use a fixed 0..1000 range."},
	)
	addResources(server)

	mcp.AddTool(server, toolDefinition("get_status", "Report configured KVM capabilities, their current runtime state, and control ownership without exposing secrets.", true, true), func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArguments) (*mcp.CallToolResult, any, error) {
		status := controller.Status(ctx)
		status.Version = version
		status.Control = control.status()
		encoded, err := json.Marshal(status)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}}, status, nil
	})

	status := controller.Status(context.Background())
	if status.Screen.Configured {
		mcp.AddTool(server, toolDefinition("get_screen", "Capture a fresh screen image; never returns a stale frame. view is original, coordinates, or both (default coordinates). format is jpeg (default) or png. JPEG quality defaults to 85 and must be 60..95; quality is forbidden for PNG. Coordinate images label a fixed top-left 0,0 to bottom-right 1000,1000 grid.", true, false), func(ctx context.Context, _ *mcp.CallToolRequest, args kvm.ScreenRequest) (*mcp.CallToolResult, any, error) {
			screen, ok := controller.(screenController)
			if !ok {
				return operationErrorResult(&kvm.OperationError{Code: kvm.ErrorInternal, Message: "screen handler is unavailable", Recovery: "restart kvmctl and inspect diagnostics"})
			}
			result, err := screen.GetScreen(ctx, args)
			if err != nil {
				return operationErrorResult(err)
			}
			content := make([]mcp.Content, 0, len(result.Images))
			for _, screenImage := range result.Images {
				content = append(content, &mcp.ImageContent{Data: screenImage.Data, MIMEType: screenImage.MIMEType})
			}
			return &mcp.CallToolResult{Content: content}, result, nil
		})
	}
	if status.Keyboard.Configured {
		mcp.AddTool(server, toolDefinition("key_press", "Press 1 to 6 non-modifier keys, optionally with ctrl, shift, alt, or meta, then automatically release every key. hold_ms defaults to 80 and must be 20..5000.", false, false), func(ctx context.Context, request *mcp.CallToolRequest, args kvm.KeyPressRequest) (*mcp.CallToolResult, any, error) {
			if err := control.acquire(request.Session); err != nil {
				return operationErrorResult(err)
			}
			controller, ok := controller.(keyPresser)
			if !ok {
				return operationErrorResult(&kvm.OperationError{Code: kvm.ErrorInternal, Message: "keyboard handler is unavailable", Recovery: "restart kvmctl and inspect diagnostics"})
			}
			if args.HoldMS == 0 {
				args.HoldMS = 80
			}
			if err := controller.KeyPress(ctx, args); err != nil {
				return operationErrorResult(err)
			}
			return successfulResult()
		})
		mcp.AddTool(server, toolDefinition("type_text", "Type 1..1024 printable ASCII characters using a US keyboard layout. Use key_press for Enter, Tab, or Backspace. interval_ms defaults to 20 and must be 0..500.", false, false), func(ctx context.Context, request *mcp.CallToolRequest, args kvm.TypeTextRequest) (*mcp.CallToolResult, any, error) {
			if err := control.acquire(request.Session); err != nil {
				return operationErrorResult(err)
			}
			controller, ok := controller.(textTyper)
			if !ok {
				return operationErrorResult(&kvm.OperationError{Code: kvm.ErrorInternal, Message: "text handler is unavailable", Recovery: "restart kvmctl and inspect diagnostics"})
			}
			if err := controller.TypeText(ctx, args); err != nil {
				return operationErrorResult(err)
			}
			return successfulResult()
		})
	}
	if status.Pointer.Absolute.Configured {
		addOperationTool(server, "mouse_move_absolute", "Move the pointer to x,y normalized coordinates in the fixed 0..1000 range.", controller, control, func(ctx context.Context, input pointerController, args kvm.AbsoluteMoveRequest) error {
			return input.MouseMoveAbsolute(ctx, args)
		})
		addOperationTool(server, "mouse_click_absolute", "Move to x,y in 0..1000 and atomically click left, right, or middle. clicks defaults to 1.", controller, control, func(ctx context.Context, input pointerController, args kvm.AbsoluteClickRequest) error {
			return input.MouseClickAbsolute(ctx, args)
		})
		addOperationTool(server, "mouse_drag_absolute", "Atomically drag from one 0..1000 position to another and release the button. duration_ms defaults to 500.", controller, control, func(ctx context.Context, input pointerController, args kvm.AbsoluteDragRequest) error {
			return input.MouseDragAbsolute(ctx, args)
		})
	}
	if status.Pointer.Relative.Configured {
		addOperationTool(server, "mouse_move_relative", "Move by dx,dy signed HID counts in -32767..32767; large movement is split into valid reports.", controller, control, func(ctx context.Context, input pointerController, args kvm.RelativeMoveRequest) error {
			return input.MouseMoveRelative(ctx, args)
		})
		addOperationTool(server, "mouse_click", "Atomically click left, right, or middle at the current pointer position. clicks defaults to 1.", controller, control, func(ctx context.Context, input pointerController, args kvm.ClickRequest) error {
			return input.MouseClick(ctx, args)
		})
		addOperationTool(server, "mouse_drag_relative", "Atomically drag by dx,dy signed HID counts and release the button. duration_ms defaults to 500.", controller, control, func(ctx context.Context, input pointerController, args kvm.RelativeDragRequest) error {
			return input.MouseDragRelative(ctx, args)
		})
		addOperationTool(server, "mouse_scroll", "Send delta vertical scroll counts in -100..100 excluding zero; positive is up.", controller, control, func(ctx context.Context, input pointerController, args kvm.ScrollRequest) error {
			return input.MouseScroll(ctx, args)
		})
	}
	if status.Keyboard.Configured || status.Pointer.Absolute.Configured || status.Pointer.Relative.Configured {
		addOperationTool(server, "release_all", "Immediately and idempotently release every key and pointer button.", controller, control, func(ctx context.Context, input pointerController, _ emptyArguments) error {
			return input.ReleaseAll(ctx)
		})
		addOperationTool(server, "release_control", "Release all input state and relinquish this MCP session's control lease.", controller, control, func(ctx context.Context, input pointerController, _ emptyArguments) error {
			return input.ReleaseControl(ctx)
		})
	}
	return server
}

func addResources(server *mcp.Server) {
	resources := map[string]struct {
		name        string
		description string
		text        string
	}{
		"kvmctl://guide": {
			name:        "kvmctl control guide",
			description: "Recommended AI control loop, coordinate system, and safety boundaries.",
			text:        "Call get_status to inspect runtime availability. Call get_screen when visual context is needed. Coordinate images and absolute pointer tools use the same normalized 0..1000 space: top-left is 0,0 and bottom-right is 1000,1000, independent of resolution. Absolute input does not require a prior screenshot or frame ID. Use atomic input tools only; every click, drag, and key press releases its input state. Use relative movement as a BIOS/UEFI fallback; its units are HID counts, not pixels.",
		},
		"kvmctl://key-names": {
			name:        "kvmctl key names",
			description: "Canonical US keyboard names accepted by key_press.",
			text:        "Modifiers: ctrl, shift, alt, meta. Letters: a through z. Digits: 0 through 9. Function keys: f1 through f12. Navigation: up, down, left, right, home, end, pageup, pagedown, insert, delete. Editing and system: enter, escape, tab, backspace, space, capslock, printscreen, scrolllock, pause, numlock. Punctuation names: minus, equal, leftbracket, rightbracket, backslash, semicolon, apostrophe, grave, comma, period, slash.",
		},
		"kvmctl://errors": {
			name:        "kvmctl errors",
			description: "Stable error codes and practical recovery actions.",
			text:        "INVALID_ARGUMENT: correct the tool arguments. CAPABILITY_NOT_CONFIGURED: enable the capability and restart or reload. DEVICE_MISSING: reconnect the device or fix its stable path. NO_VIDEO_SIGNAL: check the source HDMI output and capture input. CAPTURE_TIMEOUT: retry once, then inspect the capture device. HOST_DISCONNECTED: reconnect the USB device port to the controlled host. CONTROL_BUSY: wait for the active session lease to expire. INPUT_FAILED: inspect HID device permissions and connection. OPERATION_TIMEOUT: input was automatically released; re-observe before retrying. INTERNAL_ERROR: inspect diagnostics; do not assume the operation succeeded.",
		},
	}
	for uri, resource := range resources {
		uri, resource := uri, resource
		server.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        resource.name,
			Description: resource.description,
			MIMEType:    "text/plain",
		}, func(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			if request.Params.URI != uri {
				return nil, mcp.ResourceNotFoundError(request.Params.URI)
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/plain", Text: resource.text}}}, nil
		})
	}
}

func successfulResult() (*mcp.CallToolResult, any, error) {
	result := toolResult{OK: true}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"ok":true}`}}}, result, nil
}

func operationErrorResult(err error) (*mcp.CallToolResult, any, error) {
	var operationError *kvm.OperationError
	if !errors.As(err, &operationError) {
		operationError = &kvm.OperationError{
			Code:     kvm.ErrorInternal,
			Message:  "operation failed",
			Recovery: "inspect kvmctl diagnostics and retry",
		}
	}
	encoded, marshalErr := json.Marshal(operationError)
	if marshalErr != nil {
		return nil, nil, marshalErr
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}},
	}, operationError, nil
}

func addOperationTool[Arguments any](server *mcp.Server, name, description string, controller kvm.Controller, control *controlManager, operation func(context.Context, pointerController, Arguments) error) {
	idempotent := name == "mouse_move_absolute" || name == "release_all" || name == "release_control"
	mcp.AddTool(server, toolDefinition(name, description, false, idempotent), func(ctx context.Context, request *mcp.CallToolRequest, args Arguments) (*mcp.CallToolResult, any, error) {
		input, ok := controller.(pointerController)
		if !ok {
			return operationErrorResult(&kvm.OperationError{Code: kvm.ErrorInternal, Message: "input handler is unavailable", Recovery: "restart kvmctl and inspect diagnostics"})
		}
		if name == "release_control" {
			if err := control.ensureOwner(request.Session); err != nil {
				return operationErrorResult(err)
			}
		} else if name != "release_all" {
			if err := control.acquire(request.Session); err != nil {
				return operationErrorResult(err)
			}
		}
		if err := operation(ctx, input, args); err != nil {
			return operationErrorResult(err)
		}
		if name == "release_control" {
			control.release(request.Session)
		}
		return successfulResult()
	})
}
