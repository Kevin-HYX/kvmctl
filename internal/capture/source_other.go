//go:build !linux

package capture

import (
	"context"
	"image"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

func (source *V4L2Source) Capture(context.Context) (image.Image, error) {
	return nil, &kvm.OperationError{
		Code:     kvm.ErrorDeviceMissing,
		Message:  "V4L2 capture is only available on Linux",
		Recovery: "run kvmctl on the Linux control device",
	}
}
