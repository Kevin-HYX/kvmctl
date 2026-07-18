package hid

import (
	"fmt"
	"sync"
	"time"
)

type Keyboard struct {
	sink  Sink
	sleep func(time.Duration)
	mu    sync.Mutex
}

func NewKeyboard(sink Sink, sleep func(time.Duration)) *Keyboard {
	if sleep == nil {
		sleep = time.Sleep
	}
	return &Keyboard{sink: sink, sleep: sleep}
}

func (keyboard *Keyboard) Press(keys []string, hold time.Duration) error {
	report, err := keyboardReport(keys)
	if err != nil {
		return err
	}
	keyboard.mu.Lock()
	defer keyboard.mu.Unlock()
	pressErr := keyboard.sink.Write(report)
	if pressErr == nil {
		keyboard.sleep(hold)
	}
	releaseErr := keyboard.sink.Write(make([]byte, 8))
	if pressErr != nil {
		return pressErr
	}
	if releaseErr != nil {
		return releaseErr
	}
	return nil
}

func (keyboard *Keyboard) Release() error {
	keyboard.mu.Lock()
	defer keyboard.mu.Unlock()
	return keyboard.sink.Write(make([]byte, 8))
}

func (keyboard *Keyboard) Type(text string, interval time.Duration) error {
	keyboard.mu.Lock()
	defer keyboard.mu.Unlock()
	for index := 0; index < len(text); index++ {
		report, err := printableASCIIReport(text[index])
		if err != nil {
			_ = keyboard.sink.Write(make([]byte, 8))
			return err
		}
		if err := keyboard.sink.Write(report); err != nil {
			_ = keyboard.sink.Write(make([]byte, 8))
			return err
		}
		if err := keyboard.sink.Write(make([]byte, 8)); err != nil {
			return err
		}
		if index+1 < len(text) {
			keyboard.sleep(interval)
		}
	}
	return nil
}

func keyboardReport(keys []string) ([]byte, error) {
	report := make([]byte, 8)
	position := 2
	for _, key := range keys {
		if modifier, ok := modifierCodes[key]; ok {
			report[0] |= modifier
			continue
		}
		code, ok := keyCodes[key]
		if !ok {
			return nil, fmt.Errorf("unsupported key %q", key)
		}
		if position >= len(report) {
			return nil, fmt.Errorf("at most six non-modifier keys are supported")
		}
		report[position] = code
		position++
	}
	return report, nil
}

var modifierCodes = map[string]byte{
	"ctrl":  0x01,
	"shift": 0x02,
	"alt":   0x04,
	"meta":  0x08,
}

var keyCodes = buildKeyCodes()

func buildKeyCodes() map[string]byte {
	codes := map[string]byte{
		"enter": 0x28, "escape": 0x29, "backspace": 0x2a, "tab": 0x2b, "space": 0x2c,
		"minus": 0x2d, "equal": 0x2e, "leftbracket": 0x2f, "rightbracket": 0x30,
		"backslash": 0x31, "semicolon": 0x33, "apostrophe": 0x34, "grave": 0x35,
		"comma": 0x36, "period": 0x37, "slash": 0x38, "capslock": 0x39,
		"printscreen": 0x46, "scrolllock": 0x47, "pause": 0x48, "insert": 0x49,
		"home": 0x4a, "pageup": 0x4b, "delete": 0x4c, "end": 0x4d, "pagedown": 0x4e,
		"right": 0x4f, "left": 0x50, "down": 0x51, "up": 0x52, "numlock": 0x53,
	}
	for index := byte(0); index < 26; index++ {
		codes[string(rune('a'+index))] = 0x04 + index
	}
	for index := byte(1); index <= 9; index++ {
		codes[string(rune('0'+index))] = 0x1d + index
	}
	codes["0"] = 0x27
	for index := byte(1); index <= 12; index++ {
		codes[fmt.Sprintf("f%d", index)] = 0x39 + index
	}
	return codes
}

func printableASCIIReport(character byte) ([]byte, error) {
	if character < 0x20 || character > 0x7e {
		return nil, fmt.Errorf("character is not printable ASCII")
	}
	shift := false
	key := ""
	switch {
	case character >= 'a' && character <= 'z':
		key = string(character)
	case character >= 'A' && character <= 'Z':
		key = string(character + ('a' - 'A'))
		shift = true
	case character >= '0' && character <= '9':
		key = string(character)
	default:
		mapping, ok := asciiPunctuation[character]
		if !ok {
			return nil, fmt.Errorf("unsupported printable ASCII character %q", character)
		}
		key, shift = mapping.key, mapping.shift
	}
	report := make([]byte, 8)
	if shift {
		report[0] = modifierCodes["shift"]
	}
	report[2] = keyCodes[key]
	return report, nil
}

type asciiKey struct {
	key   string
	shift bool
}

var asciiPunctuation = map[byte]asciiKey{
	' ': {"space", false},
	'-': {"minus", false}, '_': {"minus", true},
	'=': {"equal", false}, '+': {"equal", true},
	'[': {"leftbracket", false}, '{': {"leftbracket", true},
	']': {"rightbracket", false}, '}': {"rightbracket", true},
	'\\': {"backslash", false}, '|': {"backslash", true},
	';': {"semicolon", false}, ':': {"semicolon", true},
	'\'': {"apostrophe", false}, '"': {"apostrophe", true},
	'`': {"grave", false}, '~': {"grave", true},
	',': {"comma", false}, '<': {"comma", true},
	'.': {"period", false}, '>': {"period", true},
	'/': {"slash", false}, '?': {"slash", true},
	'!': {"1", true}, '@': {"2", true}, '#': {"3", true}, '$': {"4", true}, '%': {"5", true},
	'^': {"6", true}, '&': {"7", true}, '*': {"8", true}, '(': {"9", true}, ')': {"0", true},
}
