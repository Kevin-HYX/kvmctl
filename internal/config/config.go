package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const DefaultPath = "/etc/kvmctl/config.toml"

type Config struct {
	Screen   Screen
	Keyboard Keyboard
	Pointer  Pointer
	MCP      MCP
}

type Screen struct {
	Enabled        bool
	Device         string
	CaptureTimeout time.Duration
}

type Keyboard struct {
	Enabled bool
	Device  string
}

type Pointer struct {
	AbsoluteEnabled bool
	AbsoluteDevice  string
	RelativeEnabled bool
	RelativeDevice  string
}

type MCP struct {
	ControlLease time.Duration
}

type rawConfig struct {
	Screen struct {
		Enabled        bool     `toml:"enabled"`
		Device         string   `toml:"device"`
		CaptureTimeout duration `toml:"capture_timeout"`
	} `toml:"screen"`
	Keyboard struct {
		Enabled bool   `toml:"enabled"`
		Device  string `toml:"device"`
	} `toml:"keyboard"`
	Pointer struct {
		AbsoluteEnabled bool   `toml:"absolute_enabled"`
		AbsoluteDevice  string `toml:"absolute_device"`
		RelativeEnabled bool   `toml:"relative_enabled"`
		RelativeDevice  string `toml:"relative_device"`
	} `toml:"pointer"`
	MCP struct {
		ControlLease duration `toml:"control_lease"`
	} `toml:"mcp"`
}

type duration time.Duration

func (value *duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*value = duration(parsed)
	return nil
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()

	var raw rawConfig
	decoder := toml.NewDecoder(file).DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	result := Config{
		Screen: Screen{
			Enabled:        raw.Screen.Enabled,
			Device:         raw.Screen.Device,
			CaptureTimeout: time.Duration(raw.Screen.CaptureTimeout),
		},
		Keyboard: Keyboard(raw.Keyboard),
		Pointer:  Pointer(raw.Pointer),
		MCP:      MCP{ControlLease: time.Duration(raw.MCP.ControlLease)},
	}
	if result.Screen.CaptureTimeout == 0 {
		result.Screen.CaptureTimeout = 3 * time.Second
	}
	if result.MCP.ControlLease == 0 {
		result.MCP.ControlLease = 30 * time.Second
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (config Config) Validate() error {
	if config.Screen.Enabled {
		if config.Screen.Device != "" {
			if err := validateDevice("screen.device", config.Screen.Device); err != nil {
				return err
			}
		}
		if config.Screen.CaptureTimeout < 100*time.Millisecond || config.Screen.CaptureTimeout > 30*time.Second {
			return fmt.Errorf("screen.capture_timeout must be between 100ms and 30s")
		}
	}
	if config.Keyboard.Enabled {
		if err := validateDevice("keyboard.device", config.Keyboard.Device); err != nil {
			return err
		}
	}
	if config.Pointer.AbsoluteEnabled {
		if err := validateDevice("pointer.absolute_device", config.Pointer.AbsoluteDevice); err != nil {
			return err
		}
	}
	if config.Pointer.RelativeEnabled {
		if err := validateDevice("pointer.relative_device", config.Pointer.RelativeDevice); err != nil {
			return err
		}
	}
	if config.MCP.ControlLease < time.Second || config.MCP.ControlLease > 10*time.Minute {
		return fmt.Errorf("mcp.control_lease must be between 1s and 10m")
	}
	return nil
}

func validateDevice(field, path string) error {
	if !strings.HasPrefix(path, "/dev/") || strings.Contains(path, "..") {
		return fmt.Errorf("%s must be an explicit path below /dev", field)
	}
	return nil
}
