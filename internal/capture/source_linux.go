//go:build linux

package capture

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"math"
	"sync"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/blackjack/webcam"
)

const (
	formatMJPG webcam.PixelFormat = 0x47504a4d
	formatPJPG webcam.PixelFormat = 0x47504a50
	formatYUYV webcam.PixelFormat = 0x56595559
)

type v4l2Stream struct {
	mu             sync.Mutex
	camera         *webcam.Webcam
	format         webcam.PixelFormat
	width          uint32
	height         uint32
	timeoutSeconds uint32
	closed         bool
}

func (source *V4L2Source) Capture(ctx context.Context) (image.Image, error) {
	stream, err := source.OpenVideoStream(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	frame, err := stream.Next(ctx)
	if err != nil {
		return nil, err
	}
	decoded, err := DecodeMJPEG(frame.JPEG)
	if err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the MJPEG frame was invalid", Recovery: "check capture stability and retry"}
	}
	return decoded, nil
}

func (source *V4L2Source) OpenVideoStream(ctx context.Context) (kvm.VideoStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	camera, err := webcam.Open(source.path)
	if err != nil {
		return nil, &kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "could not open the configured V4L2 screen source", Recovery: "reconnect the capture device and verify its path and permissions"}
	}
	fail := func(err error) (kvm.VideoStream, error) {
		_ = camera.Close()
		return nil, err
	}
	format, width, height, ok := chooseFormat(camera)
	if !ok {
		return fail(&kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "the V4L2 source does not offer 1920x1080 in MJPEG or YUYV", Recovery: "choose a capture device with a 1920x1080 MJPEG or YUYV mode"})
	}
	format, width, height, err = camera.SetImageFormat(format, width, height)
	if err != nil {
		return fail(&kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "could not configure the V4L2 image format", Recovery: "inspect the formats reported by kvmctl devices screen"})
	}
	if width != 1920 || height != 1080 {
		return fail(&kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "the V4L2 source did not accept 1920x1080", Recovery: "select a capture mode that provides 1920x1080 at 30 FPS"})
	}
	if err := camera.SetFramerate(30); err != nil {
		return fail(&kvm.OperationError{Code: kvm.ErrorDeviceMissing, Message: "could not configure the V4L2 source for 30 FPS", Recovery: "verify that the selected 1920x1080 format supports 30 FPS"})
	}
	if err := camera.SetBufferCount(4); err != nil {
		return fail(&kvm.OperationError{Code: kvm.ErrorInternal, Message: "could not allocate V4L2 buffers", Recovery: "inspect capture diagnostics"})
	}
	if err := camera.StartStreaming(); err != nil {
		return fail(&kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the capture device could not start streaming", Recovery: "check the source HDMI signal"})
	}
	timeoutSeconds := uint32(math.Ceil(source.timeout.Seconds()))
	if timeoutSeconds == 0 {
		timeoutSeconds = 1
	}
	if err := discardFrames(ctx, camera, timeoutSeconds, source.warmupFrames); err != nil {
		_ = camera.StopStreaming()
		return fail(warmupError(err))
	}
	return &v4l2Stream{camera: camera, format: format, width: width, height: height, timeoutSeconds: timeoutSeconds}, nil
}

func (stream *v4l2Stream) Next(ctx context.Context) (kvm.VideoFrame, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return kvm.VideoFrame{}, &kvm.OperationError{Code: kvm.ErrorHostDisconnected, Message: "the video stream is closed", Recovery: "open a new browser session"}
	}
	data, index, err := nextFrame(ctx, stream.camera, stream.timeoutSeconds)
	if err != nil {
		return kvm.VideoFrame{}, frameError(err)
	}
	defer stream.camera.ReleaseFrame(index)
	if len(data) == 0 {
		return kvm.VideoFrame{}, &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the capture device returned no current frame", Recovery: "check the source HDMI signal"}
	}
	var encoded []byte
	if stream.format == formatYUYV {
		frame, err := DecodeYUYV(data, int(stream.width), int(stream.height))
		if err != nil {
			return kvm.VideoFrame{}, &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the YUYV frame was incomplete", Recovery: "check capture stability and retry"}
		}
		var buffer bytes.Buffer
		if err := jpeg.Encode(&buffer, frame, &jpeg.Options{Quality: 85}); err != nil {
			return kvm.VideoFrame{}, &kvm.OperationError{Code: kvm.ErrorInternal, Message: "could not encode the video frame", Recovery: "inspect capture diagnostics"}
		}
		encoded = buffer.Bytes()
	} else {
		encoded = append([]byte(nil), data...)
	}
	return kvm.VideoFrame{JPEG: encoded, Width: int(stream.width), Height: int(stream.height), CapturedAt: time.Now().UTC()}, nil
}

func (stream *v4l2Stream) Close() error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return nil
	}
	stream.closed = true
	return errors.Join(stream.camera.StopStreaming(), stream.camera.Close())
}

func warmupError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, errFrameWait):
		return &kvm.OperationError{Code: kvm.ErrorCaptureTimeout, Message: "the screen source did not produce enough warmup frames", Recovery: "increase screen.capture_timeout or reduce screen.warmup_frames"}
	case errors.Is(err, errFrameRelease):
		return &kvm.OperationError{Code: kvm.ErrorInternal, Message: "the screen source could not release a warmup frame", Recovery: "inspect capture diagnostics"}
	default:
		return &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the screen source returned an invalid warmup frame", Recovery: "check capture stability and retry"}
	}
}

func frameError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, errFrameWait):
		return &kvm.OperationError{Code: kvm.ErrorCaptureTimeout, Message: "no fresh frame arrived before the capture timeout", Recovery: "retry once, then check the capture device"}
	default:
		return &kvm.OperationError{Code: kvm.ErrorNoVideoSignal, Message: "the capture device returned no current frame", Recovery: "check the source HDMI signal"}
	}
}

func chooseFormat(camera *webcam.Webcam) (webcam.PixelFormat, uint32, uint32, bool) {
	supported := camera.GetSupportedFormats()
	for _, format := range []webcam.PixelFormat{formatMJPG, formatPJPG, formatYUYV} {
		if _, ok := supported[format]; !ok {
			continue
		}
		for _, size := range camera.GetSupportedFrameSizes(format) {
			if supportsExactDimension(size.MinWidth, size.MaxWidth, size.StepWidth, 1920) &&
				supportsExactDimension(size.MinHeight, size.MaxHeight, size.StepHeight, 1080) {
				return format, 1920, 1080, true
			}
		}
	}
	return 0, 0, 0, false
}
