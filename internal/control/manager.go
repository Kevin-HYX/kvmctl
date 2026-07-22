package control

import (
	"sync"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

type Kind string

const (
	KindMCP     Kind = "mcp"
	KindBrowser Kind = "browser"
)

type Owner struct {
	Kind Kind
	ID   string
}

func MCPOwner(id string) Owner     { return Owner{Kind: KindMCP, ID: id} }
func BrowserOwner(id string) Owner { return Owner{Kind: KindBrowser, ID: id} }

type Status struct {
	State     string
	Holder    string
	ExpiresAt time.Time
}

type Manager struct {
	mu             sync.Mutex
	mcpTTL         time.Duration
	browserIdleTTL time.Duration
	releaseInput   func()
	owner          *Owner
	deadline       time.Time
	timer          *time.Timer
	generation     uint64
}

func New(mcpTTL, browserIdleTTL time.Duration, releaseInput func()) *Manager {
	return &Manager{mcpTTL: mcpTTL, browserIdleTTL: browserIdleTTL, releaseInput: releaseInput}
}

func (manager *Manager) AcquireMCP(id string) error {
	return manager.acquire(MCPOwner(id), manager.mcpTTL, false)
}

func (manager *Manager) AcquireBrowser(id string) error {
	return manager.acquire(BrowserOwner(id), manager.browserIdleTTL, true)
}

func (manager *Manager) acquire(candidate Owner, ttl time.Duration, browserMayPreempt bool) error {
	manager.mu.Lock()
	now := time.Now()
	expired := manager.owner != nil && !now.Before(manager.deadline)
	mayAcquire := manager.owner == nil || expired || *manager.owner == candidate
	if browserMayPreempt && manager.owner != nil && manager.owner.Kind == KindMCP {
		mayAcquire = true
	}
	if !mayAcquire {
		manager.mu.Unlock()
		return controlBusy(manager.owner.Kind)
	}

	release := manager.owner != nil && (expired || *manager.owner != candidate)
	manager.owner = &candidate
	manager.deadline = now.Add(ttl)
	manager.generation++
	generation := manager.generation
	if manager.timer != nil {
		manager.timer.Stop()
	}
	manager.timer = time.AfterFunc(ttl, func() { manager.expire(candidate, generation) })
	manager.mu.Unlock()
	if release && manager.releaseInput != nil {
		manager.releaseInput()
	}
	return nil
}

func (manager *Manager) Ensure(owner Owner) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.owner != nil && *manager.owner == owner && time.Now().Before(manager.deadline) {
		return nil
	}
	var holder Kind
	if manager.owner != nil {
		holder = manager.owner.Kind
	}
	return controlBusy(holder)
}

func (manager *Manager) Release(owner Owner) bool {
	manager.mu.Lock()
	if manager.owner == nil || *manager.owner != owner {
		manager.mu.Unlock()
		return false
	}
	manager.clearLocked()
	release := manager.releaseInput
	manager.mu.Unlock()
	if release != nil {
		release()
	}
	return true
}

func (manager *Manager) Status() Status {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.owner == nil || !time.Now().Before(manager.deadline) {
		return Status{State: "idle"}
	}
	return Status{State: "held", Holder: string(manager.owner.Kind), ExpiresAt: manager.deadline}
}

func (manager *Manager) expire(owner Owner, generation uint64) {
	manager.mu.Lock()
	if manager.owner == nil || *manager.owner != owner || manager.generation != generation {
		manager.mu.Unlock()
		return
	}
	manager.clearLocked()
	release := manager.releaseInput
	manager.mu.Unlock()
	if release != nil {
		release()
	}
}

func (manager *Manager) clearLocked() {
	manager.owner = nil
	manager.deadline = time.Time{}
	manager.generation++
	if manager.timer != nil {
		manager.timer.Stop()
		manager.timer = nil
	}
}

func controlBusy(holder Kind) error {
	message := "another MCP session currently controls input"
	recovery := "wait for its lease to expire or ask the current session to call release_control"
	if holder == KindBrowser {
		message = "a browser operator currently controls input"
		recovery = "wait five seconds for browser input to become idle or close the browser session"
	}
	return &kvm.OperationError{Code: kvm.ErrorControlBusy, Message: message, Recovery: recovery}
}
