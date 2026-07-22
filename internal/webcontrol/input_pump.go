package webcontrol

import (
	"context"
	"sync"
)

const maxPendingBrowserInputs = 256

type queuedBrowserInput struct {
	message     clientMessage
	coalescible bool
}

type browserInputPump struct {
	operationContext context.Context
	done             chan struct{}
	stop             chan struct{}
	wake             chan struct{}
	space            chan struct{}

	handle func(context.Context, clientMessage) error
	report func(error)

	mu              sync.Mutex
	stopOnce        sync.Once
	stopped         bool
	queue           []queuedBrowserInput
	absoluteButtons []string
	relativeButtons []string
}

func newBrowserInputPump(parent context.Context, handle func(context.Context, clientMessage) error, report func(error)) *browserInputPump {
	pump := &browserInputPump{
		operationContext: context.WithoutCancel(parent),
		done:             make(chan struct{}),
		stop:             make(chan struct{}),
		wake:             make(chan struct{}, 1),
		space:            make(chan struct{}, 1),
		handle:           handle,
		report:           report,
	}
	go pump.run()
	go func() {
		select {
		case <-parent.Done():
			pump.Stop()
		case <-pump.done:
		}
	}()
	return pump
}

func (pump *browserInputPump) Submit(message clientMessage) error {
	for {
		pump.mu.Lock()
		if pump.stopped {
			pump.mu.Unlock()
			return context.Canceled
		}

		entry := pump.classify(message)
		if message.Type == "release_all" {
			pump.queue = pump.queue[:0]
			pump.absoluteButtons = nil
			pump.relativeButtons = nil
			pump.queue = append(pump.queue, entry)
			pump.signalWake()
			pump.signalSpace()
			pump.mu.Unlock()
			return nil
		}
		if entry.coalescible && len(pump.queue) > 0 && mergeBrowserPointerInput(&pump.queue[len(pump.queue)-1], entry) {
			pump.rememberPointerButtons(message)
			pump.mu.Unlock()
			return nil
		}
		if len(pump.queue) < maxPendingBrowserInputs {
			pump.queue = append(pump.queue, entry)
			pump.rememberPointerButtons(message)
			pump.signalWake()
			pump.mu.Unlock()
			return nil
		}
		pump.mu.Unlock()

		select {
		case <-pump.stop:
			return context.Canceled
		case <-pump.space:
		}
	}
}

func (pump *browserInputPump) Stop() {
	pump.stopOnce.Do(func() {
		pump.mu.Lock()
		pump.stopped = true
		pump.queue = nil
		pump.mu.Unlock()
		close(pump.stop)
		pump.signalWake()
		pump.signalSpace()
	})
	<-pump.done
}

func (pump *browserInputPump) run() {
	defer close(pump.done)
	for {
		entry, ok := pump.next()
		if !ok {
			return
		}
		if err := pump.handle(pump.operationContext, entry.message); err != nil && !pump.isStopped() && pump.report != nil {
			pump.report(err)
		}
	}
}

func (pump *browserInputPump) next() (queuedBrowserInput, bool) {
	for {
		pump.mu.Lock()
		if pump.stopped {
			pump.mu.Unlock()
			return queuedBrowserInput{}, false
		}
		if len(pump.queue) > 0 {
			entry := pump.queue[0]
			copy(pump.queue, pump.queue[1:])
			pump.queue = pump.queue[:len(pump.queue)-1]
			pump.signalSpace()
			pump.mu.Unlock()
			return entry, true
		}
		pump.mu.Unlock()

		select {
		case <-pump.stop:
			return queuedBrowserInput{}, false
		case <-pump.wake:
		}
	}
}

func (pump *browserInputPump) classify(message clientMessage) queuedBrowserInput {
	entry := queuedBrowserInput{message: cloneClientMessage(message)}
	switch message.Type {
	case "pointer_absolute":
		buttonsChanged := !sameButtons(pump.absoluteButtons, message.Buttons)
		entry.coalescible = !buttonsChanged && message.Wheel == 0
	case "pointer_relative":
		buttonsChanged := !sameButtons(pump.relativeButtons, message.Buttons)
		entry.coalescible = !buttonsChanged && message.Wheel == 0
	}
	return entry
}

func (pump *browserInputPump) rememberPointerButtons(message clientMessage) {
	switch message.Type {
	case "pointer_absolute":
		pump.absoluteButtons = append(pump.absoluteButtons[:0], message.Buttons...)
	case "pointer_relative":
		pump.relativeButtons = append(pump.relativeButtons[:0], message.Buttons...)
	}
}

func (pump *browserInputPump) isStopped() bool {
	pump.mu.Lock()
	defer pump.mu.Unlock()
	return pump.stopped
}

func (pump *browserInputPump) signalWake() {
	select {
	case pump.wake <- struct{}{}:
	default:
	}
}

func (pump *browserInputPump) signalSpace() {
	select {
	case pump.space <- struct{}{}:
	default:
	}
}

func mergeBrowserPointerInput(pending *queuedBrowserInput, next queuedBrowserInput) bool {
	if !pending.coalescible || !next.coalescible || pending.message.Type != next.message.Type {
		return false
	}
	switch next.message.Type {
	case "pointer_absolute":
		pending.message.X = next.message.X
		pending.message.Y = next.message.Y
		return true
	case "pointer_relative":
		dx := pending.message.DX + next.message.DX
		dy := pending.message.DY + next.message.DY
		if dx < -32767 || dx > 32767 || dy < -32767 || dy > 32767 {
			return false
		}
		pending.message.DX = dx
		pending.message.DY = dy
		return true
	default:
		return false
	}
}

func cloneClientMessage(message clientMessage) clientMessage {
	message.Keys = append([]string(nil), message.Keys...)
	message.Buttons = append([]string(nil), message.Buttons...)
	return message
}

func sameButtons(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, button := range left {
		counts[button]++
	}
	for _, button := range right {
		counts[button]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
