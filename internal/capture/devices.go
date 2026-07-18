package capture

import (
	"path/filepath"
	"sort"
)

type DeviceInfo struct {
	Path    string   `json:"path"`
	Formats []string `json:"formats,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func DiscoverPaths(stablePattern, fallbackPattern string) ([]string, error) {
	stable, err := filepath.Glob(stablePattern)
	if err != nil {
		return nil, err
	}
	if len(stable) > 0 {
		sort.Strings(stable)
		return stable, nil
	}
	fallback, err := filepath.Glob(fallbackPattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(fallback)
	return fallback, nil
}

func ListDevices() ([]DeviceInfo, error) {
	paths, err := DiscoverPaths("/dev/v4l/by-id/*", "/dev/video*")
	if err != nil {
		return nil, err
	}
	devices := make([]DeviceInfo, 0, len(paths))
	for _, path := range paths {
		devices = append(devices, inspectDevice(path))
	}
	return devices, nil
}
