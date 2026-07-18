package kvm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

func TestKeyPressIsAtomicAndAlwaysReleases(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})

	err := controller.KeyPress(context.Background(), kvm.KeyPressRequest{
		Keys:   []string{"ctrl", "alt", "delete"},
		HoldMS: 80,
	})

	if err != nil {
		t.Fatalf("KeyPress() error = %v", err)
	}
	events := controller.Events()
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d; events = %#v", got, want, events)
	}
	if events[0].Kind != kvm.EventKeyPress || events[1].Kind != kvm.EventReleaseAll {
		t.Fatalf("events = %#v, want key press followed by release all", events)
	}
}

func TestTypeTextRejectsNonPrintableASCIIWithoutSendingInput(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})

	err := controller.TypeText(context.Background(), kvm.TypeTextRequest{Text: "你好"})

	var operationError *kvm.OperationError
	if !errors.As(err, &operationError) || operationError.Code != kvm.ErrorInvalidArgument {
		t.Fatalf("TypeText() error = %#v, want INVALID_ARGUMENT", err)
	}
	if events := controller.Events(); len(events) != 0 {
		t.Fatalf("events = %#v, want no input", events)
	}
}

func TestRelativeMoveSplitsCountsIntoHIDReports(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{RelativePointer: true})

	err := controller.MouseMoveRelative(context.Background(), kvm.RelativeMoveRequest{DX: 300, DY: -260})

	if err != nil {
		t.Fatalf("MouseMoveRelative() error = %v", err)
	}
	events := controller.Events()
	if got, want := len(events), 3; got != want {
		t.Fatalf("event count = %d, want %d; events = %#v", got, want, events)
	}
	var totalX, totalY int
	for _, event := range events {
		if event.Kind != kvm.EventRelativeMove {
			t.Fatalf("event = %#v, want relative move", event)
		}
		if event.DX < -127 || event.DX > 127 || event.DY < -127 || event.DY > 127 {
			t.Fatalf("event = %#v, exceeds signed HID report range", event)
		}
		totalX += event.DX
		totalY += event.DY
	}
	if totalX != 300 || totalY != -260 {
		t.Fatalf("summed movement = (%d,%d), want (300,-260)", totalX, totalY)
	}
}

func TestKeyPressRejectsUnknownKeyAsInvalidArgument(t *testing.T) {
	controller := kvm.NewMemoryController(kvm.MemoryOptions{KeyboardConfigured: true})
	err := controller.KeyPress(context.Background(), kvm.KeyPressRequest{Keys: []string{"definitely-not-a-key"}, HoldMS: 80})
	var operationError *kvm.OperationError
	if !errors.As(err, &operationError) || operationError.Code != kvm.ErrorInvalidArgument {
		t.Fatalf("error = %#v, want INVALID_ARGUMENT", err)
	}
}
