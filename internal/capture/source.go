package capture

import (
	"context"
	"image"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

type V4L2Source struct {
	path    string
	timeout time.Duration
}

func NewV4L2Source(path string, timeout time.Duration) *V4L2Source {
	return &V4L2Source{path: path, timeout: timeout}
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
