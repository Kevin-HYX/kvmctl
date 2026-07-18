package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kevin-HYX/kvmctl/internal/capture"
	"github.com/Kevin-HYX/kvmctl/internal/diagnostics"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

type operationController interface {
	kvm.Controller
	GetScreen(context.Context, kvm.ScreenRequest) (kvm.ScreenResult, error)
	KeyPress(context.Context, kvm.KeyPressRequest) error
	TypeText(context.Context, kvm.TypeTextRequest) error
	MouseMoveAbsolute(context.Context, kvm.AbsoluteMoveRequest) error
	MouseClickAbsolute(context.Context, kvm.AbsoluteClickRequest) error
	MouseDragAbsolute(context.Context, kvm.AbsoluteDragRequest) error
	MouseMoveRelative(context.Context, kvm.RelativeMoveRequest) error
	MouseClick(context.Context, kvm.ClickRequest) error
	MouseDragRelative(context.Context, kvm.RelativeDragRequest) error
	MouseScroll(context.Context, kvm.ScrollRequest) error
	ReleaseAll(context.Context) error
}

func runControllerCommand(ctx context.Context, args []string, stdout, stderr io.Writer, controller kvm.Controller) (bool, int) {
	if len(args) == 1 && args[0] == "status" {
		if controller == nil {
			return true, commandError(stderr, errors.New("controller is not configured"))
		}
		status := controller.Status(ctx)
		fmt.Fprintf(stdout, "screen: %s\nkeyboard: %s\npointer.absolute: %s\npointer.relative: %s\n", status.Screen.State, status.Keyboard.State, status.Pointer.Absolute.State, status.Pointer.Relative.State)
		return true, 0
	}
	if len(args) >= 1 && args[0] == "doctor" {
		jsonOutput := len(args) == 2 && args[1] == "--json"
		if len(args) > 2 || (len(args) == 2 && !jsonOutput) {
			return true, usageError(stderr, "usage: kvmctl doctor [--json]")
		}
		report := diagnostics.Doctor(ctx, controller)
		if jsonOutput {
			if err := json.NewEncoder(stdout).Encode(report); err != nil {
				return true, commandError(stderr, err)
			}
		} else {
			for _, check := range report.Checks {
				fmt.Fprintf(stdout, "%s: %s - %s\n", check.Name, check.State, check.Detail)
			}
		}
		if !report.Healthy {
			return true, 1
		}
		return true, 0
	}
	if len(args) >= 1 && args[0] == "devices" {
		if len(args) != 2 || args[1] != "screen" {
			return true, usageError(stderr, "usage: kvmctl devices screen")
		}
		devices, err := capture.ListDevices()
		if err != nil {
			return true, commandError(stderr, err)
		}
		if len(devices) == 0 {
			fmt.Fprintln(stdout, "no V4L2 screen sources found")
			return true, 0
		}
		for _, device := range devices {
			fmt.Fprintln(stdout, device.Path)
			for _, format := range device.Formats {
				fmt.Fprintf(stdout, "  %s\n", format)
			}
			if device.Error != "" {
				fmt.Fprintf(stdout, "  error: %s\n", device.Error)
			}
		}
		return true, 0
	}
	if len(args) == 0 {
		return false, 0
	}
	operational, ok := controller.(operationController)
	if !ok && isOperationCommand(args[0]) {
		return true, commandError(stderr, errors.New("controller operation handlers are unavailable"))
	}
	if !ok {
		return false, 0
	}

	switch args[0] {
	case "screen":
		flags := newFlagSet("screen", stderr)
		view := flags.String("view", "coordinates", "original, coordinates, or both")
		format := flags.String("format", "jpeg", "jpeg or png")
		quality := flags.Int("quality", 0, "JPEG quality 60..95")
		output := flags.String("output", "", "explicit output path")
		if flags.Parse(args[1:]) != nil || *output == "" {
			return true, commandError(stderr, errors.New("screen requires --output"))
		}
		result, err := operational.GetScreen(ctx, kvm.ScreenRequest{View: *view, Format: *format, Quality: *quality})
		if err != nil {
			return true, commandError(stderr, err)
		}
		if err := writeScreenFiles(*output, result.Images); err != nil {
			return true, commandError(stderr, err)
		}
		fmt.Fprintf(stdout, "captured %dx%d to %s\n", result.Width, result.Height, *output)
		return true, 0
	case "key":
		if len(args) < 3 || args[1] != "press" {
			return true, usageError(stderr, "usage: kvmctl key press [--hold-ms N] KEY...")
		}
		flags := newFlagSet("key press", stderr)
		hold := flags.Int("hold-ms", 80, "hold duration")
		if flags.Parse(args[2:]) != nil {
			return true, 2
		}
		return true, operationExit(stdout, stderr, operational.KeyPress(ctx, kvm.KeyPressRequest{Keys: flags.Args(), HoldMS: *hold}))
	case "text":
		if len(args) < 2 || args[1] != "type" {
			return true, usageError(stderr, "usage: kvmctl text type --text ASCII [--interval-ms N]")
		}
		flags := newFlagSet("text type", stderr)
		text := flags.String("text", "", "printable ASCII text")
		interval := flags.Int("interval-ms", 20, "character interval")
		if flags.Parse(args[2:]) != nil {
			return true, 2
		}
		return true, operationExit(stdout, stderr, operational.TypeText(ctx, kvm.TypeTextRequest{Text: *text, IntervalMS: *interval}))
	case "mouse":
		return true, runMouse(ctx, args[1:], stdout, stderr, operational)
	case "release-all":
		return true, operationExit(stdout, stderr, operational.ReleaseAll(ctx))
	}
	return false, 0
}

func runMouse(ctx context.Context, args []string, stdout, stderr io.Writer, controller operationController) int {
	if len(args) == 0 {
		return usageError(stderr, "usage: kvmctl mouse <move-absolute|click-absolute|drag-absolute|move-relative|click|drag-relative|scroll>")
	}
	flags := newFlagSet("mouse "+args[0], stderr)
	switch args[0] {
	case "move-absolute":
		x, y := flags.Int("x", -1, "x 0..1000"), flags.Int("y", -1, "y 0..1000")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		return operationExit(stdout, stderr, controller.MouseMoveAbsolute(ctx, kvm.AbsoluteMoveRequest{X: *x, Y: *y}))
	case "click-absolute":
		x, y := flags.Int("x", -1, "x 0..1000"), flags.Int("y", -1, "y 0..1000")
		button := flags.String("button", "left", "left, right, or middle")
		clicks, interval := flags.Int("clicks", 1, "click count"), flags.Int("interval-ms", 120, "click interval")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		return operationExit(stdout, stderr, controller.MouseClickAbsolute(ctx, kvm.AbsoluteClickRequest{X: *x, Y: *y, Button: *button, Clicks: *clicks, IntervalMS: *interval}))
	case "drag-absolute":
		fromX, fromY := flags.Int("from-x", -1, "start x"), flags.Int("from-y", -1, "start y")
		toX, toY := flags.Int("to-x", -1, "end x"), flags.Int("to-y", -1, "end y")
		button, duration := flags.String("button", "left", "button"), flags.Int("duration-ms", 500, "duration")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		return operationExit(stdout, stderr, controller.MouseDragAbsolute(ctx, kvm.AbsoluteDragRequest{FromX: *fromX, FromY: *fromY, ToX: *toX, ToY: *toY, Button: *button, DurationMS: *duration}))
	case "move-relative":
		dx, dy := flags.Int("dx", 0, "relative x counts"), flags.Int("dy", 0, "relative y counts")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		return operationExit(stdout, stderr, controller.MouseMoveRelative(ctx, kvm.RelativeMoveRequest{DX: *dx, DY: *dy}))
	case "click":
		button := flags.String("button", "left", "button")
		clicks, interval := flags.Int("clicks", 1, "click count"), flags.Int("interval-ms", 120, "click interval")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		return operationExit(stdout, stderr, controller.MouseClick(ctx, kvm.ClickRequest{Button: *button, Clicks: *clicks, IntervalMS: *interval}))
	case "drag-relative":
		dx, dy := flags.Int("dx", 0, "relative x counts"), flags.Int("dy", 0, "relative y counts")
		button, duration := flags.String("button", "left", "button"), flags.Int("duration-ms", 500, "duration")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		return operationExit(stdout, stderr, controller.MouseDragRelative(ctx, kvm.RelativeDragRequest{DX: *dx, DY: *dy, Button: *button, DurationMS: *duration}))
	case "scroll":
		delta := flags.Int("delta", 0, "vertical scroll counts")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		return operationExit(stdout, stderr, controller.MouseScroll(ctx, kvm.ScrollRequest{Delta: *delta}))
	default:
		return usageError(stderr, "unknown mouse operation")
	}
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(stderr)
	return set
}

func operationExit(stdout, stderr io.Writer, err error) int {
	if err != nil {
		return commandError(stderr, err)
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

func commandError(stderr io.Writer, err error) int {
	var operationError *kvm.OperationError
	if errors.As(err, &operationError) {
		fmt.Fprintf(stderr, "%s: %s; recovery: %s\n", operationError.Code, operationError.Message, operationError.Recovery)
	} else {
		fmt.Fprintf(stderr, "error: %v\n", err)
	}
	return 1
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintln(stderr, message)
	return 2
}

func isOperationCommand(command string) bool {
	return command == "screen" || command == "key" || command == "text" || command == "mouse" || command == "release-all"
}

func writeScreenFiles(output string, images []kvm.ScreenImage) error {
	if len(images) == 0 {
		return errors.New("capture returned no images")
	}
	for _, image := range images {
		path := output
		if len(images) > 1 {
			extension := filepath.Ext(output)
			path = strings.TrimSuffix(output, extension) + "-" + image.View + extension
		}
		if err := os.WriteFile(path, image.Data, 0o600); err != nil {
			return fmt.Errorf("write screenshot: %w", err)
		}
	}
	return nil
}
