//go:build linux

package capture

import (
	"context"
	"image"
	"math"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/blackjack/webcam"
)

const (
	formatMJPG webcam.PixelFormat = 0x47504a4d
	formatPJPG webcam.PixelFormat = 0x47504a50
	formatYUYV webcam.PixelFormat = 0x56595559
)

func (source *V4L2Source) Capture(ctx context.Context) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	camera, err := webcam.Open(source.path)
	if err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "could not open the configured V4L2 screen source", Recovery: "reconnect the capture device and verify its path and permissions"}
	}
	defer camera.Close()

	format, width, height, ok := chooseFormat(camera)
	if !ok {
		return nil, &kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "the V4L2 source offers neither MJPEG nor YUYV", Recovery: "choose a capture device with MJPEG or YUYV support"}
	}
	format, width, height, err = camera.SetImageFormat(format, width, height)
	if err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "could not configure the V4L2 image format", Recovery: "inspect the formats reported by kvmctl devices screen"}
	}
	if err := camera.SetBufferCount(4); err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorInternal, Message: "could not allocate V4L2 buffers", Recovery: "inspect capture diagnostics"}
	}
	if err := camera.StartStreaming(); err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the capture device could not start streaming", Recovery: "check the source HDMI signal"}
	}
	timeoutSeconds := uint32(math.Ceil(source.timeout.Seconds()))
	if timeoutSeconds == 0 {
		timeoutSeconds = 1
	}
	if err := camera.WaitForFrame(timeoutSeconds); err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorCaptureTimeout, Message: "no fresh frame arrived before the capture timeout", Recovery: "retry once, then check the capture signal and device"}
	}
	data, index, err := camera.GetFrame()
	if err != nil || len(data) == 0 {
		return nil, &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the capture device returned no current frame", Recovery: "check the source HDMI signal"}
	}
	defer camera.ReleaseFrame(index)
	if format == formatYUYV {
		frame, err := DecodeYUYV(data, int(width), int(height))
		if err != nil {
			return nil, &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the YUYV frame was incomplete", Recovery: "check capture stability and retry"}
		}
		return frame, nil
	}
	frame, err := DecodeMJPEG(data)
	if err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the MJPEG frame was invalid", Recovery: "check capture stability and retry"}
	}
	return frame, nil
}

func chooseFormat(camera *webcam.Webcam) (webcam.PixelFormat, uint32, uint32, bool) {
	supported := camera.GetSupportedFormats()
	for _, format := range []webcam.PixelFormat{formatMJPG, formatPJPG, formatYUYV} {
		if _, ok := supported[format]; !ok {
			continue
		}
		var bestWidth, bestHeight uint32
		for _, size := range camera.GetSupportedFrameSizes(format) {
			width := cappedDimension(size.MinWidth, size.MaxWidth, size.StepWidth, 1920)
			height := cappedDimension(size.MinHeight, size.MaxHeight, size.StepHeight, 1080)
			if uint64(width)*uint64(height) > uint64(bestWidth)*uint64(bestHeight) {
				bestWidth, bestHeight = width, height
			}
		}
		if bestWidth > 0 && bestHeight > 0 {
			return format, bestWidth, bestHeight, true
		}
	}
	return 0, 0, 0, false
}

func cappedDimension(minimum, maximum, step, cap uint32) uint32 {
	if maximum <= cap {
		return maximum
	}
	if cap < minimum {
		return minimum
	}
	if step == 0 {
		return minimum
	}
	return minimum + ((cap-minimum)/step)*step
}
