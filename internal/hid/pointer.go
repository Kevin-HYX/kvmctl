package hid

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

type AbsolutePointer struct {
	sink  Sink
	sleep func(time.Duration)
	mu    sync.Mutex
	x     int
	y     int
}

func NewAbsolutePointer(sink Sink, sleep func(time.Duration)) *AbsolutePointer {
	if sleep == nil {
		sleep = time.Sleep
	}
	return &AbsolutePointer{sink: sink, sleep: sleep}
}

func (pointer *AbsolutePointer) Move(x, y int) error {
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	return pointer.write(0, x, y, 0)
}

func (pointer *AbsolutePointer) Set(x, y int, buttons []string, wheel int) error {
	buttonValue, err := buttonsCode(buttons)
	if err != nil {
		return err
	}
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	return pointer.write(buttonValue, x, y, wheel)
}

func (pointer *AbsolutePointer) Click(x, y int, button string, clicks int, interval time.Duration) error {
	buttonCode, err := buttonCode(button)
	if err != nil {
		return err
	}
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	if err := pointer.write(0, x, y, 0); err != nil {
		_ = pointer.write(0, pointer.x, pointer.y, 0)
		return err
	}
	for click := 0; click < clicks; click++ {
		if err := pointer.write(buttonCode, x, y, 0); err != nil {
			_ = pointer.write(0, pointer.x, pointer.y, 0)
			return err
		}
		if err := pointer.write(0, x, y, 0); err != nil {
			return err
		}
		if click+1 < clicks {
			pointer.sleep(interval)
		}
	}
	return nil
}

func (pointer *AbsolutePointer) Drag(fromX, fromY, toX, toY int, button string, duration time.Duration) error {
	buttonCode, err := buttonCode(button)
	if err != nil {
		return err
	}
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	if err := pointer.write(0, fromX, fromY, 0); err != nil {
		_ = pointer.write(0, pointer.x, pointer.y, 0)
		return err
	}
	if err := pointer.write(buttonCode, fromX, fromY, 0); err != nil {
		_ = pointer.write(0, pointer.x, pointer.y, 0)
		return err
	}
	steps := movementSteps(duration)
	for step := 1; step <= steps; step++ {
		x := fromX + (toX-fromX)*step/steps
		y := fromY + (toY-fromY)*step/steps
		if err := pointer.write(buttonCode, x, y, 0); err != nil {
			_ = pointer.write(0, pointer.x, pointer.y, 0)
			return err
		}
		pointer.sleep(duration / time.Duration(steps))
	}
	return pointer.write(0, toX, toY, 0)
}

func (pointer *AbsolutePointer) Release() error {
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	return pointer.write(0, pointer.x, pointer.y, 0)
}

func (pointer *AbsolutePointer) write(button byte, x, y, wheel int) error {
	if err := pointer.sink.Write(absoluteReport(button, x, y, wheel)); err != nil {
		return err
	}
	pointer.x, pointer.y = x, y
	return nil
}

func absoluteReport(button byte, x, y, wheel int) []byte {
	report := make([]byte, 6)
	report[0] = button
	binary.LittleEndian.PutUint16(report[1:3], uint16(x*32767/1000))
	binary.LittleEndian.PutUint16(report[3:5], uint16(y*32767/1000))
	report[5] = byte(int8(wheel))
	return report
}

type RelativePointer struct {
	sink  Sink
	sleep func(time.Duration)
	mu    sync.Mutex
}

func NewRelativePointer(sink Sink, sleep func(time.Duration)) *RelativePointer {
	if sleep == nil {
		sleep = time.Sleep
	}
	return &RelativePointer{sink: sink, sleep: sleep}
}

func (pointer *RelativePointer) Move(dx, dy int) error {
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	for dx != 0 || dy != 0 {
		x := clamp(dx, -127, 127)
		y := clamp(dy, -127, 127)
		if err := pointer.sink.Write(relativeReport(0, x, y, 0)); err != nil {
			return err
		}
		dx -= x
		dy -= y
	}
	return nil
}

func (pointer *RelativePointer) Set(dx, dy int, buttons []string, wheel int) error {
	buttonValue, err := buttonsCode(buttons)
	if err != nil {
		return err
	}
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	first := true
	for dx != 0 || dy != 0 || first {
		x := clamp(dx, -127, 127)
		y := clamp(dy, -127, 127)
		currentWheel := 0
		if first {
			currentWheel = wheel
		}
		if err := pointer.sink.Write(relativeReport(buttonValue, x, y, currentWheel)); err != nil {
			return err
		}
		dx -= x
		dy -= y
		first = false
	}
	return nil
}

func (pointer *RelativePointer) Click(button string, clicks int, interval time.Duration) error {
	buttonValue, err := buttonCode(button)
	if err != nil {
		return err
	}
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	for click := 0; click < clicks; click++ {
		if err := pointer.sink.Write(relativeReport(buttonValue, 0, 0, 0)); err != nil {
			_ = pointer.sink.Write(make([]byte, 4))
			return err
		}
		if err := pointer.sink.Write(make([]byte, 4)); err != nil {
			return err
		}
		if click+1 < clicks {
			pointer.sleep(interval)
		}
	}
	return nil
}

func (pointer *RelativePointer) Drag(dx, dy int, button string, duration time.Duration) error {
	buttonValue, err := buttonCode(button)
	if err != nil {
		return err
	}
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	steps := movementSteps(duration)
	remainingX, remainingY := dx, dy
	for step := steps; step > 0; step-- {
		x := clamp(remainingX/step, -127, 127)
		y := clamp(remainingY/step, -127, 127)
		if err := pointer.sink.Write(relativeReport(buttonValue, x, y, 0)); err != nil {
			_ = pointer.sink.Write(make([]byte, 4))
			return err
		}
		remainingX -= x
		remainingY -= y
		pointer.sleep(duration / time.Duration(steps))
	}
	for remainingX != 0 || remainingY != 0 {
		x := clamp(remainingX, -127, 127)
		y := clamp(remainingY, -127, 127)
		if err := pointer.sink.Write(relativeReport(buttonValue, x, y, 0)); err != nil {
			_ = pointer.sink.Write(make([]byte, 4))
			return err
		}
		remainingX -= x
		remainingY -= y
	}
	return pointer.sink.Write(make([]byte, 4))
}

func (pointer *RelativePointer) Scroll(delta int) error {
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	return pointer.sink.Write(relativeReport(0, 0, 0, delta))
}

func (pointer *RelativePointer) Release() error {
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	return pointer.sink.Write(make([]byte, 4))
}

func relativeReport(button byte, dx, dy, wheel int) []byte {
	return []byte{button, byte(int8(dx)), byte(int8(dy)), byte(int8(wheel))}
}

func buttonCode(button string) (byte, error) {
	switch button {
	case "left":
		return 1, nil
	case "right":
		return 2, nil
	case "middle":
		return 4, nil
	default:
		return 0, fmt.Errorf("unsupported pointer button %q", button)
	}
}

func buttonsCode(buttons []string) (byte, error) {
	var result byte
	for _, button := range buttons {
		value, err := buttonCode(button)
		if err != nil {
			return 0, err
		}
		result |= value
	}
	return result, nil
}

func movementSteps(duration time.Duration) int {
	steps := int(duration / (10 * time.Millisecond))
	if steps < 1 {
		return 1
	}
	if steps > 100 {
		return 100
	}
	return steps
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
