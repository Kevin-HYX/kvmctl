//go:build linux

package hid

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

const hidWriteTimeout = 2 * time.Second

func writeFileReport(path string, report []byte) error {
	file, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open HID device: %w", err)
	}
	defer unix.Close(file)
	return writeNonblockingReport(file, report, hidWriteTimeout, unix.Poll)
}

func writeNonblockingReport(file int, report []byte, timeout time.Duration, poll func([]unix.PollFd, int) (int, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		written, writeErr := unix.Write(file, report)
		if writeErr == nil {
			if written != len(report) {
				return fmt.Errorf("write HID report: wrote %d of %d bytes", written, len(report))
			}
			return nil
		}
		if !errors.Is(writeErr, unix.EAGAIN) && !errors.Is(writeErr, unix.EWOULDBLOCK) {
			return fmt.Errorf("write HID report: %w", writeErr)
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("write HID report: timed out after %s waiting for the USB host", timeout)
		}
		timeoutMilliseconds := int((remaining + time.Millisecond - 1) / time.Millisecond)
		ready, pollErr := poll([]unix.PollFd{{Fd: int32(file), Events: unix.POLLOUT}}, timeoutMilliseconds)
		if pollErr != nil {
			if errors.Is(pollErr, unix.EINTR) {
				continue
			}
			return fmt.Errorf("wait for HID device: %w", pollErr)
		}
		if ready == 0 {
			return fmt.Errorf("write HID report: timed out after %s waiting for the USB host", timeout)
		}
	}
}
