package config

import (
	"fmt"
	"net/url"
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
	Server   Server
}

type Screen struct {
	Enabled        bool
	Device         string
	CaptureTimeout time.Duration
	WarmupFrames   int
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

type Server struct {
	PublicOrigin string
	WebRTC       WebRTC
}

type WebRTC struct {
	Enabled        bool
	STUNURLs       []string
	ConnectTimeout time.Duration
}

type rawConfig struct {
	Screen struct {
		Enabled        bool     `toml:"enabled"`
		Device         string   `toml:"device"`
		CaptureTimeout duration `toml:"capture_timeout"`
		WarmupFrames   *int     `toml:"warmup_frames"`
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
	Server struct {
		PublicOrigin string `toml:"public_origin"`
		WebRTC       struct {
			Enabled        *bool     `toml:"enabled"`
			STUNURLs       *[]string `toml:"stun_urls"`
			ConnectTimeout duration  `toml:"connect_timeout"`
		} `toml:"webrtc"`
	} `toml:"server"`
}

var defaultSTUNURLs = []string{"stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"}

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
			WarmupFrames:   90,
		},
		Keyboard: Keyboard(raw.Keyboard),
		Pointer:  Pointer(raw.Pointer),
		MCP:      MCP{ControlLease: time.Duration(raw.MCP.ControlLease)},
		Server: Server{
			PublicOrigin: raw.Server.PublicOrigin,
			WebRTC: WebRTC{
				Enabled:        true,
				STUNURLs:       append([]string(nil), defaultSTUNURLs...),
				ConnectTimeout: time.Duration(raw.Server.WebRTC.ConnectTimeout),
			},
		},
	}
	if raw.Server.WebRTC.Enabled != nil {
		result.Server.WebRTC.Enabled = *raw.Server.WebRTC.Enabled
	}
	if raw.Server.WebRTC.STUNURLs != nil {
		result.Server.WebRTC.STUNURLs = append([]string(nil), (*raw.Server.WebRTC.STUNURLs)...)
	}
	if result.Server.WebRTC.ConnectTimeout == 0 {
		result.Server.WebRTC.ConnectTimeout = 10 * time.Second
	}
	if result.Screen.CaptureTimeout == 0 {
		result.Screen.CaptureTimeout = 3 * time.Second
	}
	if raw.Screen.WarmupFrames != nil {
		result.Screen.WarmupFrames = *raw.Screen.WarmupFrames
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
		if config.Screen.WarmupFrames < 0 || config.Screen.WarmupFrames > 600 {
			return fmt.Errorf("screen.warmup_frames must be between 0 and 600")
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
	if config.Server.PublicOrigin != "" {
		if err := ValidatePublicOrigin(config.Server.PublicOrigin); err != nil {
			return err
		}
	}
	if config.Server.WebRTC.ConnectTimeout < time.Second || config.Server.WebRTC.ConnectTimeout > 30*time.Second {
		return fmt.Errorf("server.webrtc.connect_timeout must be between 1s and 30s")
	}
	for _, stunURL := range config.Server.WebRTC.STUNURLs {
		if err := validateSTUNURL(stunURL); err != nil {
			return err
		}
	}
	return nil
}

func validateSTUNURL(value string) error {
	scheme, address, ok := strings.Cut(value, ":")
	parsed, err := url.Parse("stun://" + address)
	if !ok || (scheme != "stun" && scheme != "stuns") || err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("server.webrtc.stun_urls entries must be STUN URLs without credentials, path, query, or fragment")
	}
	return nil
}

func ValidatePublicOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("server.public_origin must be an HTTPS origin without credentials, path, query, or fragment")
	}
	return nil
}

func validateDevice(field, path string) error {
	if !strings.HasPrefix(path, "/dev/") || strings.Contains(path, "..") {
		return fmt.Errorf("%s must be an explicit path below /dev", field)
	}
	return nil
}
