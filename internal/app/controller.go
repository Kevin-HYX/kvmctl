package app

import (
	"errors"
	"os"

	"github.com/Kevin-HYX/kvmctl/internal/capture"
	"github.com/Kevin-HYX/kvmctl/internal/config"
	"github.com/Kevin-HYX/kvmctl/internal/hid"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

func BuildController(configuration config.Config) *kvm.HardwareController {
	options := kvm.HardwareOptions{}
	if configuration.Screen.Enabled {
		screenPath := configuration.Screen.Device
		if screenPath == "" {
			paths, err := capture.DiscoverPaths("/dev/v4l/by-id/*", "/dev/video*")
			switch {
			case err != nil:
				options.Screen = capture.NewUnavailableSource(kvm.ErrorInternal, "screen source discovery failed", "inspect device discovery diagnostics")
				options.ScreenProbe = errorProbe(err)
			case len(paths) == 0:
				options.Screen = capture.NewUnavailableSource(kvm.ErrorDeviceMissing, "no V4L2 screen source was found", "connect a capture device or configure screen.device")
				options.ScreenProbe = errorProbe(errors.New("no screen source"))
			case len(paths) > 1:
				options.Screen = capture.NewUnavailableSource(kvm.ErrorDeviceMissing, "multiple V4L2 screen sources require an explicit selection", "set screen.device to a stable /dev/v4l/by-id path")
				options.ScreenProbe = errorProbe(errors.New("multiple screen sources"))
			default:
				screenPath = paths[0]
			}
		}
		if screenPath != "" {
			options.Screen = capture.NewV4L2Source(screenPath, configuration.Screen.CaptureTimeout)
			options.ScreenProbe = fileProbe(screenPath)
		}
	}
	if configuration.Keyboard.Enabled {
		options.Keyboard = hid.NewKeyboard(hid.FileSink{Path: configuration.Keyboard.Device}, nil)
		options.KeyboardProbe = fileProbe(configuration.Keyboard.Device)
	}
	if configuration.Pointer.AbsoluteEnabled {
		options.AbsolutePointer = hid.NewAbsolutePointer(hid.FileSink{Path: configuration.Pointer.AbsoluteDevice}, nil)
		options.AbsolutePointerProbe = fileProbe(configuration.Pointer.AbsoluteDevice)
	}
	if configuration.Pointer.RelativeEnabled {
		options.RelativePointer = hid.NewRelativePointer(hid.FileSink{Path: configuration.Pointer.RelativeDevice}, nil)
		options.RelativePointerProbe = fileProbe(configuration.Pointer.RelativeDevice)
	}
	return kvm.NewHardwareController(options)
}

func errorProbe(err error) func() error {
	return func() error { return err }
}

func fileProbe(path string) func() error {
	return func() error {
		_, err := os.Stat(path)
		return err
	}
}
