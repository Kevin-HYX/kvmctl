package kvm_test

import (
	"context"
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

type fakeKeyboard struct {
	pressed  int
	released int
}

func (keyboard *fakeKeyboard) Press([]string, time.Duration) error {
	keyboard.pressed++
	return nil
}

func (keyboard *fakeKeyboard) Type(string, time.Duration) error { return nil }
func (keyboard *fakeKeyboard) Set([]string) error               { return nil }
func (keyboard *fakeKeyboard) Release() error {
	keyboard.released++
	return nil
}

func TestHardwareControllerValidatesThenUsesKeyboardDriver(t *testing.T) {
	keyboard := &fakeKeyboard{}
	controller := kvm.NewHardwareController(kvm.HardwareOptions{Keyboard: keyboard})

	if err := controller.KeyPress(context.Background(), kvm.KeyPressRequest{Keys: []string{"escape"}, HoldMS: 80}); err != nil {
		t.Fatalf("KeyPress(): %v", err)
	}
	if keyboard.pressed != 1 {
		t.Fatalf("pressed = %d, want 1", keyboard.pressed)
	}
	if err := controller.KeyPress(context.Background(), kvm.KeyPressRequest{Keys: []string{"escape"}, HoldMS: 10}); err == nil {
		t.Fatal("invalid KeyPress() error = nil")
	}
	if keyboard.pressed != 1 {
		t.Fatalf("invalid request reached driver; pressed = %d", keyboard.pressed)
	}
}
