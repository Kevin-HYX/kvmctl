package hid

import (
	"fmt"
	"os"
)

type Sink interface {
	Write([]byte) error
}

type FileSink struct {
	Path string
}

func (sink FileSink) Write(report []byte) error {
	file, err := os.OpenFile(sink.Path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open HID device: %w", err)
	}
	defer file.Close()
	written, err := file.Write(report)
	if err != nil {
		return fmt.Errorf("write HID report: %w", err)
	}
	if written != len(report) {
		return fmt.Errorf("write HID report: wrote %d of %d bytes", written, len(report))
	}
	return nil
}
