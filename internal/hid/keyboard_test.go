package hid_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/hid"
)

type recordingSink struct {
	reports [][]byte
	failAt  int
}

func (sink *recordingSink) Write(report []byte) error {
	sink.reports = append(sink.reports, append([]byte(nil), report...))
	if sink.failAt > 0 && len(sink.reports) == sink.failAt {
		return errors.New("write failed")
	}
	return nil
}

func TestKeyboardPressWritesUSBReportThenRelease(t *testing.T) {
	sink := &recordingSink{}
	keyboard := hid.NewKeyboard(sink, func(time.Duration) {})

	if err := keyboard.Press([]string{"ctrl", "alt", "delete"}, 80*time.Millisecond); err != nil {
		t.Fatalf("Press(): %v", err)
	}
	if len(sink.reports) != 2 {
		t.Fatalf("reports = %#v, want press and release", sink.reports)
	}
	wantPress := []byte{0x05, 0x00, 0x4c, 0, 0, 0, 0, 0}
	if string(sink.reports[0]) != string(wantPress) {
		t.Fatalf("press report = %v, want %v", sink.reports[0], wantPress)
	}
	if string(sink.reports[1]) != string(make([]byte, 8)) {
		t.Fatalf("release report = %v, want all zero", sink.reports[1])
	}
}

func TestKeyboardAttemptsReleaseWhenPressWriteFails(t *testing.T) {
	sink := &recordingSink{failAt: 1}
	keyboard := hid.NewKeyboard(sink, func(time.Duration) {})

	if err := keyboard.Press([]string{"escape"}, 80*time.Millisecond); err == nil {
		t.Fatal("Press() error = nil, want write failure")
	}
	if len(sink.reports) != 2 || string(sink.reports[1]) != string(make([]byte, 8)) {
		t.Fatalf("reports = %#v, want failed press followed by release attempt", sink.reports)
	}
}

func TestKeyboardTypesPrintableASCIIUsingUSLayout(t *testing.T) {
	sink := &recordingSink{}
	keyboard := hid.NewKeyboard(sink, func(time.Duration) {})

	if err := keyboard.Type("A!", 20*time.Millisecond); err != nil {
		t.Fatalf("Type(): %v", err)
	}
	want := [][]byte{
		{0x02, 0, 0x04, 0, 0, 0, 0, 0},
		make([]byte, 8),
		{0x02, 0, 0x1e, 0, 0, 0, 0, 0},
		make([]byte, 8),
	}
	if len(sink.reports) != len(want) {
		t.Fatalf("reports = %#v, want %#v", sink.reports, want)
	}
	for index := range want {
		if string(sink.reports[index]) != string(want[index]) {
			t.Fatalf("report %d = %v, want %v", index, sink.reports[index], want[index])
		}
	}
}
