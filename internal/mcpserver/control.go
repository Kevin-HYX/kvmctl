package mcpserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/control"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type controlManager struct {
	arbiter *control.Manager
	mu      sync.Mutex
	watched map[*mcp.ServerSession]struct{}
}

func newControlManager(ttl time.Duration, release func()) *controlManager {
	return newControlManagerWithArbiter(control.New(ttl, 5*time.Second, release))
}

func newControlManagerWithArbiter(arbiter *control.Manager) *controlManager {
	return &controlManager{arbiter: arbiter, watched: make(map[*mcp.ServerSession]struct{})}
}

func (manager *controlManager) acquire(session *mcp.ServerSession) error {
	if err := manager.arbiter.AcquireMCP(sessionID(session)); err != nil {
		return err
	}
	manager.watch(session)
	return nil
}

func (manager *controlManager) ensureOwner(session *mcp.ServerSession) error {
	return manager.arbiter.Ensure(control.MCPOwner(sessionID(session)))
}

func (manager *controlManager) release(session *mcp.ServerSession) {
	manager.arbiter.Release(control.MCPOwner(sessionID(session)))
}

func (manager *controlManager) status() kvm.ControlStatus {
	status := manager.arbiter.Status()
	result := kvm.ControlStatus{State: status.State, Holder: status.Holder}
	if !status.ExpiresAt.IsZero() {
		result.ExpiresAt = status.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func (manager *controlManager) watch(session *mcp.ServerSession) {
	if session == nil {
		return
	}
	manager.mu.Lock()
	if _, exists := manager.watched[session]; exists {
		manager.mu.Unlock()
		return
	}
	manager.watched[session] = struct{}{}
	manager.mu.Unlock()
	go func() {
		_ = session.Wait()
		manager.mu.Lock()
		delete(manager.watched, session)
		manager.mu.Unlock()
		manager.arbiter.Release(control.MCPOwner(sessionID(session)))
	}()
}

func sessionID(session *mcp.ServerSession) string {
	return fmt.Sprintf("%p", session)
}

func releaseControllerInput(controller kvm.Controller) func() {
	return func() {
		if input, ok := controller.(interface{ ReleaseAll(context.Context) error }); ok {
			_ = input.ReleaseAll(context.Background())
		}
	}
}
