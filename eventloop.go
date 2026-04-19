package espresso

import (
	"sync"
	"time"
)

// ─── Event Loop ─────────────────────────────────────────
// Turns espresso from a JS evaluator into a JS runtime.
// Processes timers (setTimeout/setInterval) and queued callbacks
// until no more work remains — like Node.js.

// EventLoop manages async callbacks, timers, and I/O events.
type EventLoop struct {
	mu       sync.Mutex
	vm       *VM
	timers   map[int]*loopTimer
	nextID   int
	queue    []func() // pending callbacks to execute
	running  bool
	stopCh   chan struct{}
	wakeupCh chan struct{}
	refs     int // reference count — loop stays alive while refs > 0
}

type loopTimer struct {
	id       int
	fn       *Value
	interval time.Duration
	repeat   bool
	timer    *time.Timer
	ticker   *time.Ticker
	done     chan struct{} // signals ticker goroutine to stop
}

// NewEventLoop creates an event loop bound to a VM.
func NewEventLoop(vm *VM) *EventLoop {
	return &EventLoop{
		vm:       vm,
		timers:   make(map[int]*loopTimer),
		stopCh:   make(chan struct{}),
		wakeupCh: make(chan struct{}, 64),
	}
}

// RegisterGlobals injects setTimeout, setInterval, clearTimeout,
// clearInterval into the VM scope.
func (el *EventLoop) RegisterGlobals() {
	el.vm.RegisterFunc("setTimeout", func(args []*Value) *Value {
		if len(args) < 1 {
			return Undefined
		}
		fn := args[0]
		delay := 0.0
		if len(args) > 1 {
			delay = args[1].toNum()
		}
		id := el.SetTimeout(fn, time.Duration(delay)*time.Millisecond)
		return newNum(float64(id))
	})

	el.vm.RegisterFunc("setInterval", func(args []*Value) *Value {
		if len(args) < 2 {
			return Undefined
		}
		fn := args[0]
		interval := args[1].toNum()
		if interval < 1 {
			interval = 1
		}
		id := el.SetInterval(fn, time.Duration(interval)*time.Millisecond)
		return newNum(float64(id))
	})

	el.vm.RegisterFunc("clearTimeout", func(args []*Value) *Value {
		if len(args) > 0 {
			el.ClearTimer(int(args[0].toNum()))
		}
		return Undefined
	})

	el.vm.RegisterFunc("clearInterval", func(args []*Value) *Value {
		if len(args) > 0 {
			el.ClearTimer(int(args[0].toNum()))
		}
		return Undefined
	})
}

// SetTimeout schedules fn to run after delay. Returns timer ID.
func (el *EventLoop) SetTimeout(fn *Value, delay time.Duration) int {
	el.mu.Lock()
	el.nextID++
	id := el.nextID
	lt := &loopTimer{
		id: id,
		fn: fn,
	}
	lt.timer = time.AfterFunc(delay, func() {
		el.enqueue(func() {
			el.callAndSync(lt.fn)
			el.mu.Lock()
			delete(el.timers, id)
			el.refs--
			el.mu.Unlock()
		})
	})
	el.timers[id] = lt
	el.refs++
	el.mu.Unlock()
	return id
}

// SetInterval schedules fn to run every interval. Returns timer ID.
func (el *EventLoop) SetInterval(fn *Value, interval time.Duration) int {
	el.mu.Lock()
	el.nextID++
	id := el.nextID
	lt := &loopTimer{
		id:       id,
		fn:       fn,
		repeat:   true,
		interval: interval,
	}
	lt.ticker = time.NewTicker(interval)
	lt.done = make(chan struct{})
	el.timers[id] = lt
	el.refs++
	el.mu.Unlock()

	go func() {
		for {
			select {
			case <-lt.done:
				return
			case <-lt.ticker.C:
				el.enqueue(func() {
					el.callAndSync(lt.fn)
				})
			}
		}
	}()

	return id
}

// callAndSync calls a function value and syncs any scope mutations
// from arrow function captured scopes back to the VM scope.
func (el *EventLoop) callAndSync(fn *Value) {
	callFuncValue(fn, nil, el.vm.scope)
	// Arrow functions capture their own scope; sync changes back to VM
	if fn != nil && fn.str == "__arrow" {
		arrowRegistryMu.Lock()
		af, ok := arrowRegistry[int(fn.num)]
		arrowRegistryMu.Unlock()
		if ok && af.scope != nil {
			for k, v := range af.scope {
				el.vm.scope[k] = v
			}
		}
	}
}

// ClearTimer cancels a timeout or interval.
func (el *EventLoop) ClearTimer(id int) {
	el.mu.Lock()
	defer el.mu.Unlock()
	lt, ok := el.timers[id]
	if !ok {
		return
	}
	if lt.timer != nil {
		lt.timer.Stop()
	}
	if lt.ticker != nil {
		lt.ticker.Stop()
	}
	if lt.done != nil {
		close(lt.done)
	}
	delete(el.timers, id)
	el.refs--
}

// Enqueue adds a callback to run on the next loop iteration.
// Safe to call from any goroutine.
func (el *EventLoop) Enqueue(fn func()) {
	el.enqueue(fn)
}

func (el *EventLoop) enqueue(fn func()) {
	el.mu.Lock()
	el.queue = append(el.queue, fn)
	el.mu.Unlock()
	// Non-blocking wakeup
	select {
	case el.wakeupCh <- struct{}{}:
	default:
	}
}

// Ref increments the reference count — loop stays alive while refs > 0.
func (el *EventLoop) Ref() {
	el.mu.Lock()
	el.refs++
	el.mu.Unlock()
}

// Unref decrements the reference count.
func (el *EventLoop) Unref() {
	el.mu.Lock()
	el.refs--
	el.mu.Unlock()
	select {
	case el.wakeupCh <- struct{}{}:
	default:
	}
}

// Run starts the event loop. Blocks until Stop is called or
// there are no more timers/refs keeping it alive.
func (el *EventLoop) Run() {
	el.mu.Lock()
	el.running = true
	el.mu.Unlock()

	for {
		// Drain the callback queue
		for {
			el.mu.Lock()
			if !el.running {
				el.mu.Unlock()
				return
			}
			if len(el.queue) == 0 {
				el.mu.Unlock()
				break
			}
			// Take all pending callbacks
			batch := el.queue
			el.queue = nil
			el.mu.Unlock()

			for _, fn := range batch {
				fn()
				// Check if Stop was called from inside the callback
				el.mu.Lock()
				stopped := !el.running
				el.mu.Unlock()
				if stopped {
					return
				}
			}
		}

		// Check if we should exit
		el.mu.Lock()
		if !el.running || (el.refs <= 0 && len(el.queue) == 0) {
			el.running = false
			el.mu.Unlock()
			return
		}
		el.mu.Unlock()

		// Wait for wakeup or stop
		select {
		case <-el.wakeupCh:
		case <-el.stopCh:
			return
		}
	}
}

// Stop signals the event loop to exit.
func (el *EventLoop) Stop() {
	el.mu.Lock()
	if !el.running {
		el.mu.Unlock()
		return
	}
	el.running = false
	// Clean up all timers
	for id, lt := range el.timers {
		if lt.timer != nil {
			lt.timer.Stop()
		}
		if lt.ticker != nil {
			lt.ticker.Stop()
		}
		if lt.done != nil {
			close(lt.done)
		}
		delete(el.timers, id)
	}
	el.refs = 0
	el.mu.Unlock()
	select {
	case el.stopCh <- struct{}{}:
	default:
	}
}
