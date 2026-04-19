package espresso

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// ─── Stdio Support ──────────────────────────────────────
// Provides process.stdin, process.stdout, process.stderr
// with Node.js-compatible APIs for line-based and raw I/O.

// StdioConfig configures I/O streams for the runtime.
// Defaults to os.Stdin/Stdout/Stderr when nil.
type StdioConfig struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// RegisterStdio sets up process.stdin, process.stdout, process.stderr
// on an existing process object in the VM scope. Requires an event loop
// for async stdin reading.
func RegisterStdio(vm *VM, el *EventLoop, cfg *StdioConfig) {
	if cfg == nil {
		cfg = &StdioConfig{}
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}

	proc := vm.Get("process")
	if proc.IsUndefined() {
		proc = NewObj(make(map[string]*Value))
		vm.SetValue("process", proc)
	}

	// ── stdout ──
	stdout := NewObj(map[string]*Value{
		"write": NewNativeFunc(func(args []*Value) *Value {
			if len(args) > 0 {
				fmt.Fprint(cfg.Stdout, args[0].toStr())
			}
			return True
		}),
		"once": NewNativeFunc(func(args []*Value) *Value {
			// once('drain', fn) — stdout is never buffered in espresso, call immediately
			if len(args) >= 2 {
				callFuncValue(args[1], nil, el.vm.copyScope())
			}
			return Undefined
		}),
	})
	proc.object["stdout"] = stdout

	// ── stderr ──
	stderr := NewObj(map[string]*Value{
		"write": NewNativeFunc(func(args []*Value) *Value {
			if len(args) > 0 {
				fmt.Fprint(cfg.Stderr, args[0].toStr())
			}
			return True
		}),
	})
	proc.object["stderr"] = stderr

	// ── stdin ──
	stdin := newStdinObject(cfg.Stdin, el)
	proc.object["stdin"] = stdin
}

// newStdinObject creates a process.stdin object with on('data'|'line', cb) support.
func newStdinObject(reader io.Reader, el *EventLoop) *Value {
	var dataListeners []*Value
	var lineListeners []*Value
	var endListeners []*Value
	var reading bool

	stdinObj := NewObj(make(map[string]*Value))

	// on(event, callback) — register event listeners
	stdinObj.object["on"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 {
			return stdinObj
		}
		event := args[0].toStr()
		cb := args[1]
		switch event {
		case "data":
			dataListeners = append(dataListeners, cb)
		case "line":
			lineListeners = append(lineListeners, cb)
		case "end":
			endListeners = append(endListeners, cb)
		}

		// Start reading on first listener registration
		if !reading && (len(dataListeners) > 0 || len(lineListeners) > 0) {
			reading = true
			el.Ref() // Keep event loop alive

			go func() {
				scanner := bufio.NewScanner(reader)
				// Allow up to 1MB lines (for large JSON payloads)
				scanner.Buffer(make([]byte, 64*1024), 1024*1024)
				for scanner.Scan() {
					line := scanner.Text()
					el.Enqueue(func() {
						lineVal := NewStr(line)
						for _, cb := range lineListeners {
							callFuncValue(cb, []*Value{lineVal}, el.vm.copyScope())
						}
						if len(dataListeners) > 0 {
							dataVal := newBufferValue([]byte(line + "\n"))
							for _, cb := range dataListeners {
								callFuncValue(cb, []*Value{dataVal}, el.vm.copyScope())
							}
						}
					})
				}
				// EOF or error — fire end event
				el.Enqueue(func() {
					for _, cb := range endListeners {
						callFuncValue(cb, nil, el.vm.copyScope())
					}
				})
				el.Unref()
			}()
		}

		return stdinObj // chainable
	})

	// resume() — no-op (auto-starts on first listener), for Node.js compat
	stdinObj.object["resume"] = NewNativeFunc(func(args []*Value) *Value {
		return stdinObj
	})

	// setEncoding() — no-op, we always use UTF-8
	stdinObj.object["setEncoding"] = NewNativeFunc(func(args []*Value) *Value {
		return stdinObj
	})

	// off(event, callback) — remove event listener
	stdinObj.object["off"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 { return stdinObj }
		event := args[0].toStr()
		cb := args[1]
		switch event {
		case "data":
			for i, l := range dataListeners {
				if l == cb { dataListeners = append(dataListeners[:i], dataListeners[i+1:]...); break }
			}
		case "line":
			for i, l := range lineListeners {
				if l == cb { lineListeners = append(lineListeners[:i], lineListeners[i+1:]...); break }
			}
		case "end":
			for i, l := range endListeners {
				if l == cb { endListeners = append(endListeners[:i], endListeners[i+1:]...); break }
			}
		}
		return stdinObj
	})

	// once(event, callback) — register one-time listener
	stdinObj.object["once"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) < 2 { return stdinObj }
		// Wrap as one-shot — call cb then remove
		event := args[0].toStr()
		cb := args[1]
		var wrapper *Value
		wrapper = NewNativeFunc(func(wargs []*Value) *Value {
			callFuncValue(cb, wargs, el.vm.copyScope())
			// Remove self
			switch event {
			case "data":
				for i, l := range dataListeners {
					if l == wrapper { dataListeners = append(dataListeners[:i], dataListeners[i+1:]...); break }
				}
			case "line":
				for i, l := range lineListeners {
					if l == wrapper { lineListeners = append(lineListeners[:i], lineListeners[i+1:]...); break }
				}
			case "end":
				for i, l := range endListeners {
					if l == wrapper { endListeners = append(endListeners[:i], endListeners[i+1:]...); break }
				}
			}
			return Undefined
		})
		switch event {
		case "data":
			dataListeners = append(dataListeners, wrapper)
		case "line":
			lineListeners = append(lineListeners, wrapper)
		case "end":
			endListeners = append(endListeners, wrapper)
		}
		return stdinObj
	})

	// pause() — no-op (we can't stop the goroutine scanner)
	stdinObj.object["pause"] = NewNativeFunc(func(args []*Value) *Value {
		return stdinObj
	})

	// listenerCount(event) — return number of listeners
	stdinObj.object["listenerCount"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return newNum(0) }
		switch args[0].toStr() {
		case "data":
			return newNum(float64(len(dataListeners)))
		case "line":
			return newNum(float64(len(lineListeners)))
		case "end":
			return newNum(float64(len(endListeners)))
		}
		return newNum(0)
	})

	return stdinObj
}
