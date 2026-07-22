package webcontrol_test

import (
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/webcontrol"
)

func TestEntryIsSingleUseAndBecomesConnectedSession(t *testing.T) {
	manager := webcontrol.NewSessionManager(webcontrol.SessionOptions{
		PublicOrigin:   "https://120.26.145.91:18443",
		EntryTTL:       time.Second,
		ReconnectGrace: time.Second,
	})
	entry, err := manager.Open()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(entry.EntryURL)
	if err != nil {
		t.Fatal(err)
	}
	token := parsed.Query().Get("token")
	if token == "" || strings.Contains(parsed.Path, token) {
		t.Fatalf("entry URL = %q, want query token", entry.EntryURL)
	}
	cookie, err := manager.Redeem(token)
	if err != nil {
		t.Fatalf("Redeem(): %v", err)
	}
	if _, err := manager.Redeem(token); err == nil {
		t.Fatal("entry token was redeemable twice")
	}
	connectionID, err := manager.Connect(cookie)
	if err != nil {
		t.Fatalf("Connect(): %v", err)
	}
	if got := manager.Status(); got.State != webcontrol.StateConnected {
		t.Fatalf("status = %#v, want connected", got)
	}
	manager.Disconnect(connectionID)
	if got := manager.Status(); got.State != webcontrol.StateReconnecting {
		t.Fatalf("status = %#v, want reconnecting", got)
	}
	if _, err := manager.Connect(cookie); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
}

func TestDirectTransportReportsConnectingConnectedAndFailedStates(t *testing.T) {
	manager := webcontrol.NewSessionManager(webcontrol.SessionOptions{
		PublicOrigin:   "https://example.invalid:18443",
		EntryTTL:       time.Second,
		ReconnectGrace: time.Second,
	})
	entry, err := manager.Open()
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(entry.EntryURL)
	cookie, err := manager.Redeem(parsed.Query().Get("token"))
	if err != nil {
		t.Fatal(err)
	}
	connectionID, err := manager.BeginTransport(cookie, webcontrol.TransportDirect, func() {})
	if err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if status.State != webcontrol.StateReconnecting || status.Transport != webcontrol.TransportDirect || status.TransportState != webcontrol.TransportStateConnecting || status.RetryAt == nil {
		t.Fatalf("connecting status = %#v", status)
	}
	if err := manager.MarkTransportConnected(connectionID); err != nil {
		t.Fatal(err)
	}
	status = manager.Status()
	if status.State != webcontrol.StateConnected || status.TransportState != webcontrol.TransportStateConnected || status.RetryAt != nil {
		t.Fatalf("connected status = %#v", status)
	}
	manager.FailTransport(connectionID, "WEBRTC_DISCONNECTED")
	status = manager.Status()
	if status.State != webcontrol.StateReconnecting || status.TransportState != webcontrol.TransportStateFailed || status.ErrorCode != "WEBRTC_DISCONNECTED" || status.RetryAt == nil {
		t.Fatalf("failed status = %#v", status)
	}
}

func TestOpenReturnsBusyWithoutRevealingExistingEntry(t *testing.T) {
	manager := webcontrol.NewSessionManager(webcontrol.SessionOptions{
		PublicOrigin:   "https://example.invalid:18443",
		EntryTTL:       time.Second,
		ReconnectGrace: time.Second,
	})
	if _, err := manager.Open(); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Open()
	busy, ok := err.(*webcontrol.BusyError)
	if !ok || busy.Code != "BROWSER_SESSION_BUSY" || busy.State != webcontrol.StateEntryPending {
		t.Fatalf("error = %#v, want structured busy", err)
	}
	if strings.Contains(err.Error(), "token=") {
		t.Fatalf("busy error leaked entry URL: %v", err)
	}
}

func TestReconnectGraceExpiresAndReleasesSession(t *testing.T) {
	var ended atomic.Int32
	manager := webcontrol.NewSessionManager(webcontrol.SessionOptions{
		PublicOrigin:   "https://example.invalid:18443",
		EntryTTL:       time.Second,
		ReconnectGrace: 30 * time.Millisecond,
		OnSessionEnd:   func() { ended.Add(1) },
	})
	entry, err := manager.Open()
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(entry.EntryURL)
	cookie, err := manager.Redeem(parsed.Query().Get("token"))
	if err != nil {
		t.Fatal(err)
	}
	connectionID, err := manager.Connect(cookie)
	if err != nil {
		t.Fatal(err)
	}
	manager.Disconnect(connectionID)
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != webcontrol.StateNone && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := manager.Status().State; got != webcontrol.StateNone {
		t.Fatalf("state = %s, want none", got)
	}
	if got := ended.Load(); got != 1 {
		t.Fatalf("session end callbacks = %d, want 1", got)
	}
	if _, err := manager.Connect(cookie); err == nil {
		t.Fatal("expired session cookie reconnected")
	}
}

func TestCloseRevokesPendingEntryOrActiveSession(t *testing.T) {
	manager := webcontrol.NewSessionManager(webcontrol.SessionOptions{
		PublicOrigin:   "https://example.invalid:18443",
		EntryTTL:       time.Second,
		ReconnectGrace: time.Second,
	})
	if _, err := manager.Open(); err != nil {
		t.Fatal(err)
	}
	if !manager.Close() || manager.Status().State != webcontrol.StateNone {
		t.Fatal("Close() did not revoke pending entry")
	}
	if manager.Close() {
		t.Fatal("Close() reported a session when none existed")
	}
}

func TestCloseDisconnectsActiveTransport(t *testing.T) {
	manager := webcontrol.NewSessionManager(webcontrol.SessionOptions{
		PublicOrigin:   "https://example.invalid:18443",
		EntryTTL:       time.Second,
		ReconnectGrace: time.Second,
	})
	entry, err := manager.Open()
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(entry.EntryURL)
	cookie, err := manager.Redeem(parsed.Query().Get("token"))
	if err != nil {
		t.Fatal(err)
	}
	disconnected := make(chan struct{}, 1)
	if _, err := manager.Connect(cookie, func() { disconnected <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	if !manager.Close() {
		t.Fatal("Close() = false, want active session")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("active transport was not disconnected")
	}
}
