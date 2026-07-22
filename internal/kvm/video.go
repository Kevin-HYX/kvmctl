package kvm

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"sync"
	"time"
)

type VideoFrame struct {
	JPEG       []byte
	Width      int
	Height     int
	CapturedAt time.Time
}

type VideoStream interface {
	Next(context.Context) (VideoFrame, error)
	Close() error
}

type VideoStreamer interface {
	OpenVideoStream(context.Context) (VideoStream, error)
}

func (controller *HardwareController) OpenVideoStream(ctx context.Context) (VideoStream, error) {
	if controller.screen == nil {
		return nil, capabilityNotConfigured("screen")
	}
	streamer, ok := controller.screen.(VideoStreamer)
	if !ok {
		return nil, &OperationError{Code: ErrorInternal, Message: "the screen source does not support interactive video", Recovery: "configure a streaming screen source"}
	}
	return streamer.OpenVideoStream(ctx)
}

type memoryVideoStream struct {
	mu     sync.Mutex
	closed bool
	frame  VideoFrame
}

func (controller *MemoryController) OpenVideoStream(context.Context) (VideoStream, error) {
	if !controller.status.Screen.Configured {
		return nil, capabilityNotConfigured("screen")
	}
	controller.mu.Lock()
	frame := controller.screen
	controller.mu.Unlock()
	if frame == nil {
		return nil, &OperationError{Code: ErrorNoVideoSignal, Message: "the screen source has no current frame", Recovery: "check the HDMI source and capture device"}
	}
	data, _, err := encodeScreen(frame, "jpeg", 85)
	if err != nil {
		return nil, err
	}
	return &memoryVideoStream{frame: VideoFrame{JPEG: data, Width: frame.Bounds().Dx(), Height: frame.Bounds().Dy(), CapturedAt: time.Now().UTC()}}, nil
}

func (stream *memoryVideoStream) Next(ctx context.Context) (VideoFrame, error) {
	if err := ctx.Err(); err != nil {
		return VideoFrame{}, err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return VideoFrame{}, errors.New("video stream is closed")
	}
	frame := stream.frame
	frame.CapturedAt = time.Now().UTC()
	frame.JPEG = append([]byte(nil), frame.JPEG...)
	if _, err := jpeg.DecodeConfig(bytes.NewReader(frame.JPEG)); err != nil {
		return VideoFrame{}, err
	}
	return frame, nil
}

func (stream *memoryVideoStream) Close() error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.closed = true
	return nil
}
