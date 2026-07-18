package mcpserver_test

import (
	"testing"

	"github.com/Kevin-HYX/kvmctl/internal/mcpserver"
)

func TestValidateLoopbackListenRejectsNonLoopbackAddresses(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8080", "192.168.1.10:8080", "[::]:8080", ":8080"} {
		if err := mcpserver.ValidateLoopbackListen(address); err == nil {
			t.Errorf("ValidateLoopbackListen(%q) error = nil, want rejection", address)
		}
	}
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		if err := mcpserver.ValidateLoopbackListen(address); err != nil {
			t.Errorf("ValidateLoopbackListen(%q) error = %v, want success", address, err)
		}
	}
}
