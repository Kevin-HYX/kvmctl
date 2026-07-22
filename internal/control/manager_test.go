package control_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/control"
)

func TestMCPControlIsExclusive(t *testing.T) {
	manager := control.New(time.Second, time.Second, nil)
	if err := manager.AcquireMCP("one"); err != nil {
		t.Fatalf("AcquireMCP(one): %v", err)
	}
	if err := manager.AcquireMCP("two"); err == nil {
		t.Fatal("AcquireMCP(two) error = nil, want busy")
	}
	if got := manager.Status(); got.State != "held" || got.Holder != "mcp" {
		t.Fatalf("status = %#v, want MCP holder", got)
	}
}

func TestBrowserPreemptsMCPAndBlocksItUntilIdle(t *testing.T) {
	var releases atomic.Int32
	manager := control.New(time.Second, 30*time.Millisecond, func() { releases.Add(1) })
	if err := manager.AcquireMCP("agent"); err != nil {
		t.Fatal(err)
	}
	if err := manager.AcquireBrowser("human"); err != nil {
		t.Fatalf("AcquireBrowser(): %v", err)
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("releases after preemption = %d, want 1", got)
	}
	if err := manager.AcquireMCP("agent"); err == nil {
		t.Fatal("MCP acquired while browser active")
	}

	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "idle" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if manager.Status().State != "idle" {
		t.Fatal("browser control did not expire")
	}
	if got := releases.Load(); got != 2 {
		t.Fatalf("releases after idle = %d, want 2", got)
	}
	if err := manager.AcquireMCP("agent"); err != nil {
		t.Fatalf("MCP could not acquire after browser idle: %v", err)
	}
}

func TestOnlyOwningBrowserCanRenewOrRelease(t *testing.T) {
	var releases atomic.Int32
	manager := control.New(time.Second, time.Second, func() { releases.Add(1) })
	if err := manager.AcquireBrowser("one"); err != nil {
		t.Fatal(err)
	}
	if err := manager.AcquireBrowser("two"); err == nil {
		t.Fatal("second browser acquired control")
	}
	if manager.Release(control.BrowserOwner("two")) {
		t.Fatal("non-owner released control")
	}
	if !manager.Release(control.BrowserOwner("one")) {
		t.Fatal("owner did not release control")
	}
	if got := releases.Load(); got != 1 {
		t.Fatalf("release calls = %d, want 1", got)
	}
}
