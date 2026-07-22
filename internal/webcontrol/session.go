package webcontrol

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
)

type State string

type Transport string

type TransportState string

const (
	StateNone         State = "none"
	StateEntryPending State = "entry_pending"
	StateConnected    State = "connected"
	StateReconnecting State = "reconnecting"
)

const (
	TransportNone    Transport = "none"
	TransportDirect  Transport = "direct"
	TransportRelayed Transport = "relayed"
)

const (
	TransportStateNone       TransportState = "none"
	TransportStateConnecting TransportState = "connecting"
	TransportStateConnected  TransportState = "connected"
	TransportStateFailed     TransportState = "failed"
)

type SessionOptions struct {
	PublicOrigin   string
	EntryTTL       time.Duration
	ReconnectGrace time.Duration
	Random         io.Reader
	OnSessionEnd   func()
}

type Entry struct {
	EntryURL  string    `json:"entry_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Status struct {
	State          State          `json:"state"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	RetryAt        *time.Time     `json:"retry_at,omitempty"`
	Transport      Transport      `json:"transport"`
	TransportState TransportState `json:"transport_state"`
	ErrorCode      string         `json:"error_code,omitempty"`
}

type BusyError struct {
	Code    string     `json:"code"`
	State   State      `json:"state"`
	RetryAt *time.Time `json:"retry_at"`
}

func (err *BusyError) Error() string {
	return fmt.Sprintf("%s: browser session state is %s", err.Code, err.State)
}

var ErrInvalidEntry = errors.New("invalid browser entry")
var ErrInvalidSession = errors.New("invalid browser session")
var ErrAlreadyConnected = errors.New("browser session is already connected")

type SessionManager struct {
	mu             sync.Mutex
	publicOrigin   string
	entryTTL       time.Duration
	reconnectGrace time.Duration
	random         io.Reader
	onSessionEnd   func()
	state          State
	entryHash      [sha256.Size]byte
	entryExpiresAt time.Time
	sessionHash    [sha256.Size]byte
	connectionID   string
	disconnect     func()
	retryAt        time.Time
	timer          *time.Timer
	generation     uint64
	transport      Transport
	transportState TransportState
	errorCode      string
}

func NewSessionManager(options SessionOptions) *SessionManager {
	if options.EntryTTL <= 0 {
		options.EntryTTL = 10 * time.Minute
	}
	if options.ReconnectGrace <= 0 {
		options.ReconnectGrace = time.Minute
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	return &SessionManager{
		publicOrigin:   strings.TrimRight(options.PublicOrigin, "/"),
		entryTTL:       options.EntryTTL,
		reconnectGrace: options.ReconnectGrace,
		random:         options.Random,
		onSessionEnd:   options.OnSessionEnd,
		state:          StateNone,
		transport:      TransportNone,
		transportState: TransportStateNone,
	}
}

func (manager *SessionManager) Open() (Entry, error) {
	manager.mu.Lock()
	ended := manager.expireLocked(time.Now())
	if manager.state != StateNone {
		status := manager.statusLocked()
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return Entry{}, &BusyError{Code: "BROWSER_SESSION_BUSY", State: status.State, RetryAt: status.RetryAt}
	}
	token, err := manager.randomToken()
	if err != nil {
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return Entry{}, fmt.Errorf("generate browser entry: %w", err)
	}
	manager.entryHash = sha256.Sum256([]byte(token))
	manager.entryExpiresAt = time.Now().Add(manager.entryTTL)
	manager.state = StateEntryPending
	manager.generation++
	generation := manager.generation
	manager.scheduleLocked(manager.entryTTL, func() { manager.expireEntry(generation) })
	expiresAt := manager.entryExpiresAt
	entryURL := manager.publicOrigin + "/control/enter?token=" + url.QueryEscape(token)
	manager.mu.Unlock()
	manager.notifySessionEnd(ended)
	return Entry{EntryURL: entryURL, ExpiresAt: expiresAt.UTC()}, nil
}

func (manager *SessionManager) Redeem(token string) (string, error) {
	manager.mu.Lock()
	ended := manager.expireLocked(time.Now())
	candidate := sha256.Sum256([]byte(token))
	if manager.state != StateEntryPending || subtle.ConstantTimeCompare(candidate[:], manager.entryHash[:]) != 1 {
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return "", ErrInvalidEntry
	}
	sessionToken, err := manager.randomToken()
	if err != nil {
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return "", fmt.Errorf("generate browser session: %w", err)
	}
	manager.entryHash = [sha256.Size]byte{}
	manager.entryExpiresAt = time.Time{}
	manager.sessionHash = sha256.Sum256([]byte(sessionToken))
	manager.state = StateReconnecting
	manager.retryAt = time.Now().Add(manager.reconnectGrace)
	manager.generation++
	generation := manager.generation
	manager.scheduleLocked(manager.reconnectGrace, func() { manager.expireSession(generation) })
	manager.mu.Unlock()
	manager.notifySessionEnd(ended)
	return sessionToken, nil
}

func (manager *SessionManager) Connect(sessionToken string, disconnect ...func()) (string, error) {
	return manager.ConnectTransport(sessionToken, TransportRelayed, disconnect...)
}

func (manager *SessionManager) ConnectTransport(sessionToken string, transport Transport, disconnect ...func()) (string, error) {
	connectionID, err := manager.BeginTransport(sessionToken, transport, disconnect...)
	if err != nil {
		return "", err
	}
	if err := manager.MarkTransportConnected(connectionID); err != nil {
		manager.FailTransport(connectionID, "TRANSPORT_CONNECTION_FAILED")
		return "", err
	}
	return connectionID, nil
}

func (manager *SessionManager) BeginTransport(sessionToken string, transport Transport, disconnect ...func()) (string, error) {
	manager.mu.Lock()
	ended := manager.expireLocked(time.Now())
	if !manager.validSessionLocked(sessionToken) {
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return "", ErrInvalidSession
	}
	if manager.state == StateConnected || manager.connectionID != "" {
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return "", ErrAlreadyConnected
	}
	connectionID, err := manager.randomToken()
	if err != nil {
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return "", fmt.Errorf("generate connection identifier: %w", err)
	}
	manager.connectionID = connectionID
	if len(disconnect) > 0 {
		manager.disconnect = disconnect[0]
	}
	manager.transport = transport
	manager.transportState = TransportStateConnecting
	manager.errorCode = ""
	manager.mu.Unlock()
	manager.notifySessionEnd(ended)
	return connectionID, nil
}

func (manager *SessionManager) MarkTransportConnected(connectionID string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.connectionID != connectionID || manager.transportState != TransportStateConnecting {
		return ErrInvalidSession
	}
	manager.state = StateConnected
	manager.transportState = TransportStateConnected
	manager.errorCode = ""
	manager.retryAt = time.Time{}
	manager.generation++
	if manager.timer != nil {
		manager.timer.Stop()
		manager.timer = nil
	}
	return nil
}

func (manager *SessionManager) FailTransport(connectionID, errorCode string) {
	manager.mu.Lock()
	if manager.connectionID != connectionID {
		manager.mu.Unlock()
		return
	}
	manager.connectionID = ""
	manager.disconnect = nil
	manager.state = StateReconnecting
	manager.transportState = TransportStateFailed
	manager.errorCode = errorCode
	if manager.retryAt.IsZero() {
		manager.retryAt = time.Now().Add(manager.reconnectGrace)
		manager.generation++
		generation := manager.generation
		manager.scheduleLocked(manager.reconnectGrace, func() { manager.expireSession(generation) })
	}
	manager.mu.Unlock()
}

func (manager *SessionManager) ValidateSession(sessionToken string) bool {
	manager.mu.Lock()
	ended := manager.expireLocked(time.Now())
	valid := manager.validSessionLocked(sessionToken)
	manager.mu.Unlock()
	manager.notifySessionEnd(ended)
	return valid
}

func (manager *SessionManager) Disconnect(connectionID string) {
	manager.mu.Lock()
	if manager.connectionID != connectionID {
		manager.mu.Unlock()
		return
	}
	wasConnected := manager.state == StateConnected
	manager.connectionID = ""
	manager.disconnect = nil
	manager.state = StateReconnecting
	manager.transportState = TransportStateFailed
	if wasConnected || manager.retryAt.IsZero() {
		manager.retryAt = time.Now().Add(manager.reconnectGrace)
		manager.generation++
		generation := manager.generation
		manager.scheduleLocked(manager.reconnectGrace, func() { manager.expireSession(generation) })
	}
	manager.mu.Unlock()
}

func (manager *SessionManager) Close() bool {
	manager.mu.Lock()
	ended := manager.expireLocked(time.Now())
	if manager.state == StateNone {
		manager.mu.Unlock()
		manager.notifySessionEnd(ended)
		return false
	}
	hadSession := manager.state != StateEntryPending
	disconnect := manager.disconnect
	manager.clearLocked()
	onEnd := manager.onSessionEnd
	manager.mu.Unlock()
	if disconnect != nil {
		disconnect()
	}
	if (ended || hadSession) && onEnd != nil {
		onEnd()
	}
	return true
}

func (manager *SessionManager) Status() Status {
	manager.mu.Lock()
	ended := manager.expireLocked(time.Now())
	status := manager.statusLocked()
	manager.mu.Unlock()
	manager.notifySessionEnd(ended)
	return status
}

func (manager *SessionManager) statusLocked() Status {
	status := Status{State: manager.state, Transport: manager.transport, TransportState: manager.transportState, ErrorCode: manager.errorCode}
	switch manager.state {
	case StateEntryPending:
		expires := manager.entryExpiresAt.UTC()
		status.ExpiresAt = &expires
		status.RetryAt = &expires
	case StateReconnecting:
		retry := manager.retryAt.UTC()
		status.RetryAt = &retry
	}
	return status
}

func (manager *SessionManager) validSessionLocked(token string) bool {
	if manager.state != StateConnected && manager.state != StateReconnecting {
		return false
	}
	candidate := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(candidate[:], manager.sessionHash[:]) == 1
}

func (manager *SessionManager) expireLocked(now time.Time) bool {
	switch manager.state {
	case StateEntryPending:
		if !now.Before(manager.entryExpiresAt) {
			manager.clearLocked()
		}
	case StateReconnecting:
		if !now.Before(manager.retryAt) {
			manager.clearLocked()
			return true
		}
	}
	return false
}

func (manager *SessionManager) expireEntry(generation uint64) {
	manager.mu.Lock()
	if manager.generation == generation && manager.state == StateEntryPending {
		manager.clearLocked()
	}
	manager.mu.Unlock()
}

func (manager *SessionManager) expireSession(generation uint64) {
	manager.mu.Lock()
	if manager.generation != generation || manager.state != StateReconnecting {
		manager.mu.Unlock()
		return
	}
	manager.clearLocked()
	onEnd := manager.onSessionEnd
	manager.mu.Unlock()
	if onEnd != nil {
		onEnd()
	}
}

func (manager *SessionManager) clearLocked() {
	manager.state = StateNone
	manager.entryHash = [sha256.Size]byte{}
	manager.entryExpiresAt = time.Time{}
	manager.sessionHash = [sha256.Size]byte{}
	manager.connectionID = ""
	manager.disconnect = nil
	manager.retryAt = time.Time{}
	manager.transport = TransportNone
	manager.transportState = TransportStateNone
	manager.errorCode = ""
	manager.generation++
	if manager.timer != nil {
		manager.timer.Stop()
		manager.timer = nil
	}
}

func (manager *SessionManager) scheduleLocked(after time.Duration, callback func()) {
	if manager.timer != nil {
		manager.timer.Stop()
	}
	manager.timer = time.AfterFunc(after, callback)
}

func (manager *SessionManager) notifySessionEnd(ended bool) {
	if ended && manager.onSessionEnd != nil {
		manager.onSessionEnd()
	}
}

func (manager *SessionManager) randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := io.ReadFull(manager.random, data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
