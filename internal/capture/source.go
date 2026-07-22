package capture

import (
	"context"
	"errors"
	"fmt"
	"image"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

type V4L2Source struct {
	path         string
	timeout      time.Duration
	warmupFrames int
}

func NewV4L2Source(path string, timeout time.Duration, warmupFrames int) *V4L2Source {
	return &V4L2Source{path: path, timeout: timeout, warmupFrames: warmupFrames}
}

var (
	errFrameWait    = errors.New("wait for frame")
	errFrameRead    = errors.New("read frame")
	errFrameRelease = errors.New("release frame")
)

type frameReader interface {
	WaitForFrame(uint32) error
	GetFrame() ([]byte, uint32, error)
	ReleaseFrame(uint32) error
}

func nextFrame(ctx context.Context, reader frameReader, timeoutSeconds uint32) ([]byte, uint32, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if err := reader.WaitForFrame(timeoutSeconds); err != nil {
		return nil, 0, fmt.Errorf("%w: %v", errFrameWait, err)
	}
	data, bufferIndex, err := reader.GetFrame()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", errFrameRead, err)
	}
	return data, bufferIndex, nil
}

func discardFrames(ctx context.Context, reader frameReader, timeoutSeconds uint32, count int) error {
	for index := 0; index < count; index++ {
		data, bufferIndex, err := nextFrame(ctx, reader, timeoutSeconds)
		if err != nil {
			return err
		}
		if err := reader.ReleaseFrame(bufferIndex); err != nil {
			return fmt.Errorf("%w: %v", errFrameRelease, err)
		}
		if len(data) == 0 {
			return fmt.Errorf("%w: empty frame", errFrameRead)
		}
	}
	return nil
}

func supportsExactDimension(minimum, maximum, step, target uint32) bool {
	if target < minimum || target > maximum {
		return false
	}
	if minimum == maximum || step == 0 {
		return true
	}
	return (target-minimum)%step == 0
}

type UnavailableSource struct {
	err *kvm.OperationError
}

func NewUnavailableSource(code kvm.ErrorCode, message, recovery string) *UnavailableSource {
	return &UnavailableSource{err: &kvm.OperationError{Code: code, Message: message, Recovery: recovery}}
}

func (source *UnavailableSource) Capture(context.Context) (image.Image, error) {
	return nil, source.err
}
