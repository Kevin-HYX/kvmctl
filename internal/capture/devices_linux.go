//go:build linux

package capture

import (
	"fmt"
	"sort"

	"github.com/blackjack/webcam"
)

func inspectDevice(path string) DeviceInfo {
	info := DeviceInfo{Path: path}
	camera, err := webcam.Open(path)
	if err != nil {
		info.Error = "cannot open device"
		return info
	}
	defer camera.Close()
	formats := camera.GetSupportedFormats()
	for format, name := range formats {
		if format != formatMJPG && format != formatPJPG && format != formatYUYV {
			continue
		}
		for _, size := range camera.GetSupportedFrameSizes(format) {
			info.Formats = append(info.Formats, fmt.Sprintf("%s %s", name, size.GetString()))
		}
	}
	sort.Strings(info.Formats)
	return info
}
