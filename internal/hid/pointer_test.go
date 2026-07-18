package hid_test

import (
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/hid"
)

func TestAbsolutePointerUsesNormalizedCoordinates(t *testing.T) {
	sink := &recordingSink{}
	pointer := hid.NewAbsolutePointer(sink, func(time.Duration) {})

	if err := pointer.Move(1000, 500); err != nil {
		t.Fatalf("Move(): %v", err)
	}
	want := []byte{0, 0xff, 0x7f, 0xff, 0x3f, 0}
	if len(sink.reports) != 1 || string(sink.reports[0]) != string(want) {
		t.Fatalf("reports = %v, want [%v]", sink.reports, want)
	}
	if err := pointer.Release(); err != nil {
		t.Fatalf("Release(): %v", err)
	}
	if got := sink.reports[len(sink.reports)-1]; string(got) != string(want) {
		t.Fatalf("release report = %v, want same position with buttons released: %v", got, want)
	}
}

func TestRelativePointerSplitsMovementAndClickReleases(t *testing.T) {
	sink := &recordingSink{}
	pointer := hid.NewRelativePointer(sink, func(time.Duration) {})

	if err := pointer.Move(300, -260); err != nil {
		t.Fatalf("Move(): %v", err)
	}
	if len(sink.reports) != 3 {
		t.Fatalf("move reports = %#v, want 3", sink.reports)
	}
	if err := pointer.Click("left", 1, 120*time.Millisecond); err != nil {
		t.Fatalf("Click(): %v", err)
	}
	if got := sink.reports[len(sink.reports)-2][0]; got != 1 {
		t.Fatalf("button report = %d, want left button bit", got)
	}
	if got := sink.reports[len(sink.reports)-1]; string(got) != string(make([]byte, 4)) {
		t.Fatalf("release report = %v, want zeros", got)
	}
}
