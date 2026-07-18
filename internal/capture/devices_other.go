//go:build !linux

package capture

func inspectDevice(path string) DeviceInfo {
	return DeviceInfo{Path: path, Error: "V4L2 inspection is only available on Linux"}
}
