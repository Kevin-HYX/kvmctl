package mcpserver

import (
	"context"
	"sync"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type controlManager struct {
	mu           sync.Mutex
	owner        *mcp.ServerSession
	deadline     time.Time
	ttl          time.Duration
	timer        *time.Timer
	releaseInput func()
	watched      map[*mcp.ServerSession]struct{}
	generation   uint64
}

func newControlManager(ttl time.Duration, release func()) *controlManager {
	return &controlManager{ttl: ttl, releaseInput: release, watched: make(map[*mcp.ServerSession]struct{})}
}

func (manager *controlManager) acquire(session *mcp.ServerSession) error {
	manager.mu.Lock()
	now := time.Now()
	expired := manager.owner != nil && now.After(manager.deadline)
	if manager.owner == nil || expired || manager.owner == session {
		if expired && manager.releaseInput != nil {
			manager.releaseInput()
		}
		manager.owner = session
		manager.deadline = now.Add(manager.ttl)
		manager.generation++
		generation := manager.generation
		if manager.timer != nil {
			manager.timer.Stop()
		}
		manager.timer = time.AfterFunc(manager.ttl, func() { manager.expire(session, generation) })
		if _, ok := manager.watched[session]; !ok && session != nil {
			manager.watched[session] = struct{}{}
			go func() {
				_ = session.Wait()
				manager.sessionClosed(session)
			}()
		}
		manager.mu.Unlock()
		return nil
	}
	manager.mu.Unlock()
	return controlBusy()
}

func (manager *controlManager) ensureOwner(session *mcp.ServerSession) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.owner == session && time.Now().Before(manager.deadline) {
		return nil
	}
	return controlBusy()
}

func (manager *controlManager) release(session *mcp.ServerSession) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.owner == session {
		manager.generation++
		if manager.timer != nil {
			manager.timer.Stop()
			manager.timer = nil
		}
		manager.owner = nil
		manager.deadline = time.Time{}
	}
}

func (manager *controlManager) status() kvm.ControlStatus {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.owner == nil || time.Now().After(manager.deadline) {
		return kvm.ControlStatus{State: "idle"}
	}
	return kvm.ControlStatus{State: "held", ExpiresAt: manager.deadline.UTC().Format(time.RFC3339Nano)}
}

func (manager *controlManager) expire(session *mcp.ServerSession, generation uint64) {
	manager.mu.Lock()
	if manager.owner != session || manager.generation != generation {
		manager.mu.Unlock()
		return
	}
	manager.owner = nil
	manager.generation++
	manager.deadline = time.Time{}
	manager.timer = nil
	release := manager.releaseInput
	manager.mu.Unlock()
	if release != nil {
		release()
	}
}

func (manager *controlManager) sessionClosed(session *mcp.ServerSession) {
	manager.mu.Lock()
	delete(manager.watched, session)
	if manager.owner != session {
		manager.mu.Unlock()
		return
	}
	if manager.timer != nil {
		manager.timer.Stop()
	}
	manager.owner = nil
	manager.generation++
	manager.deadline = time.Time{}
	manager.timer = nil
	release := manager.releaseInput
	manager.mu.Unlock()
	if release != nil {
		release()
	}
}

func releaseControllerInput(controller kvm.Controller) func() {
	return func() {
		if input, ok := controller.(interface{ ReleaseAll(context.Context) error }); ok {
			_ = input.ReleaseAll(context.Background())
		}
	}
}

func controlBusy() error {
	return &kvm.OperationError{
		Code:     kvm.ErrorControlBusy,
		Message:  "another MCP session currently controls input",
		Recovery: "wait for its lease to expire or ask the current session to call release_control",
	}
}
