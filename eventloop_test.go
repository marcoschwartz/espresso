package espresso

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Event Loop Tests ───────────────────────────────────

func TestEventLoop_SetTimeout(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	vm.Run(`
		var called = false;
		setTimeout(() => {
			called = true;
		}, 10);
	`)

	el.Run()

	r := vm.Get("called")
	if !r.Truthy() {
		t.Error("setTimeout callback should have been called")
	}
}

func TestEventLoop_SetTimeoutOrder(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	vm.Set("order", []interface{}{})
	vm.RegisterFunc("pushOrder", func(args []*Value) *Value {
		cur := vm.Get("order")
		cur.array = append(cur.array, args[0])
		return Undefined
	})

	vm.Run(`
		setTimeout(() => { pushOrder("second"); }, 30);
		setTimeout(() => { pushOrder("first"); }, 10);
	`)

	el.Run()

	order := vm.Get("order")
	if order.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", order.Len())
	}
	if order.Array()[0].String() != "first" {
		t.Errorf("expected 'first' first, got '%s'", order.Array()[0].String())
	}
	if order.Array()[1].String() != "second" {
		t.Errorf("expected 'second' second, got '%s'", order.Array()[1].String())
	}
}

func TestEventLoop_ClearTimeout(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	vm.Run(`
		var called = false;
		var id = setTimeout(() => {
			called = true;
		}, 50);
		clearTimeout(id);
	`)

	// Give it time to fire (it shouldn't)
	go func() {
		time.Sleep(100 * time.Millisecond)
		el.Stop()
	}()
	el.Run()

	r := vm.Get("called")
	if r.Truthy() {
		t.Error("cleared timeout should not have been called")
	}
}

func TestEventLoop_SetInterval(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	count := 0
	vm.RegisterFunc("tick", func(args []*Value) *Value {
		count++
		if count >= 3 {
			el.Stop()
		}
		return Undefined
	})

	vm.Run(`
		setInterval(() => { tick(); }, 15);
	`)

	done := make(chan struct{})
	go func() {
		el.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("event loop timed out")
		el.Stop()
	}

	if count < 3 {
		t.Errorf("expected count >= 3, got %d", count)
	}
}

func TestEventLoop_ClearInterval(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	vm.Set("count", 0)
	vm.RegisterFunc("incCount", func(args []*Value) *Value {
		c := vm.Get("count").Number() + 1
		vm.Set("count", c)
		return Undefined
	})

	vm.Run(`
		var id = setInterval(() => { incCount(); }, 10);
		setTimeout(() => { clearInterval(id); }, 55);
	`)

	go func() {
		time.Sleep(200 * time.Millisecond)
		el.Stop()
	}()
	el.Run()

	c := vm.Get("count").Number()
	if c > 6 {
		t.Errorf("interval should have been cleared, but count is %v", c)
	}
}

func TestEventLoop_ZeroDelay(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	vm.Run(`
		var result = "none";
		setTimeout(() => { result = "done"; }, 0);
	`)

	el.Run()

	r := vm.Get("result")
	if r.String() != "done" {
		t.Errorf("expected 'done', got '%s'", r.String())
	}
}

func TestEventLoop_NestedTimeouts(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	vm.Run(`
		var result = "";
		setTimeout(() => {
			result = result + "a";
			setTimeout(() => {
				result = result + "b";
			}, 10);
		}, 10);
	`)

	el.Run()

	r := vm.Get("result")
	if r.String() != "ab" {
		t.Errorf("expected 'ab', got '%s'", r.String())
	}
}

func TestEventLoop_ExitsWhenEmpty(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	// No timers — should exit immediately
	done := make(chan struct{})
	go func() {
		el.Run()
		close(done)
	}()

	select {
	case <-done:
		// good
	case <-time.After(time.Second):
		t.Error("event loop should have exited immediately with no refs")
		el.Stop()
	}
}

// ─── Stdio Tests ────────────────────────────────────────

func TestStdio_StdoutWrite(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	var buf bytes.Buffer

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(""),
		Stdout: &buf,
		Stderr: &bytes.Buffer{},
	})

	vm.Run(`
		process.stdout.write("hello ");
		process.stdout.write("world");
	`)

	if buf.String() != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", buf.String())
	}
}

func TestStdio_StderrWrite(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	var buf bytes.Buffer

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
		Stderr: &buf,
	})

	vm.Run(`
		process.stderr.write("error msg");
	`)

	if buf.String() != "error msg" {
		t.Errorf("expected 'error msg', got '%s'", buf.String())
	}
}

func TestStdio_StdinLine(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)

	input := "line1\nline2\nline3\n"

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(input),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})

	var mu sync.Mutex
	var lines []string
	vm.RegisterFunc("collectLine", func(args []*Value) *Value {
		mu.Lock()
		lines = append(lines, args[0].String())
		mu.Unlock()
		return Undefined
	})

	vm.Run(`
		process.stdin.on("line", (line) => {
			collectLine(line);
		});
	`)

	el.Run()

	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestStdio_StdinData(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	RegisterBuffer(vm)

	input := "hello\nworld\n"
	var outBuf bytes.Buffer

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(input),
		Stdout: &outBuf,
		Stderr: &bytes.Buffer{},
	})

	vm.Run(`
		process.stdin.on("data", (chunk) => {
			process.stdout.write(chunk.toString());
		});
	`)

	el.Run()

	// data events pass Buffer chunks that include the newline
	if outBuf.String() != "hello\nworld\n" {
		t.Errorf("expected 'hello\\nworld\\n', got '%s'", outBuf.String())
	}
}

func TestStdio_StdinEnd(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader("x\n"),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})

	vm.Set("ended", false)
	vm.RegisterFunc("setEnded", func(args []*Value) *Value {
		vm.Set("ended", true)
		return Undefined
	})

	vm.Run(`
		process.stdin.on("line", (line) => {});
		process.stdin.on("end", () => { setEnded(); });
	`)

	el.Run()

	if !vm.Get("ended").Truthy() {
		t.Error("end event should have fired")
	}
}

func TestStdio_StdinLineEchoServer(t *testing.T) {
	// Simulates a simple echo server: reads lines, writes them back
	vm := New()
	el := NewEventLoop(vm)

	input := "ping\npong\n"
	var outBuf bytes.Buffer

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(input),
		Stdout: &outBuf,
		Stderr: &bytes.Buffer{},
	})

	vm.Run(`
		process.stdin.on("line", (line) => {
			process.stdout.write("echo: " + line + "\n");
		});
	`)

	el.Run()

	expected := "echo: ping\necho: pong\n"
	if outBuf.String() != expected {
		t.Errorf("expected %q, got %q", expected, outBuf.String())
	}
}

func TestStdio_JSONRPCEcho(t *testing.T) {
	// Simulates reading JSON-RPC messages from stdin and responding on stdout
	// This is the exact pattern MCP servers use
	vm := New()
	el := NewEventLoop(vm)

	input := `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"name":"world"}}` + "\n"
	var outBuf bytes.Buffer

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(input),
		Stdout: &outBuf,
		Stderr: &bytes.Buffer{},
	})

	vm.Run(`
		process.stdin.on("line", (line) => {
			const msg = JSON.parse(line);
			const response = JSON.stringify({
				jsonrpc: "2.0",
				id: msg.id,
				result: { greeting: "Hello, " + msg.params.name + "!" }
			});
			process.stdout.write(response + "\n");
		});
	`)

	el.Run()

	out := strings.TrimSpace(outBuf.String())
	if !strings.Contains(out, `"Hello, world!"`) {
		t.Errorf("expected greeting in output, got: %s", out)
	}
	if !strings.Contains(out, `"jsonrpc":"2.0"`) {
		t.Errorf("expected jsonrpc in output, got: %s", out)
	}
	if !strings.Contains(out, `"id":1`) {
		t.Errorf("expected id in output, got: %s", out)
	}
}

func TestStdio_StdinMethods(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	RegisterBuffer(vm)
	var outBuf bytes.Buffer
	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(""),
		Stdout: &outBuf,
		Stderr: &bytes.Buffer{},
	})
	vm.Run(`
		const hasOff = typeof process.stdin.off === "function";
		const hasOnce = typeof process.stdin.once === "function";
		const hasPause = typeof process.stdin.pause === "function";
		const hasLC = typeof process.stdin.listenerCount === "function";
		const hasStdoutOnce = typeof process.stdout.once === "function";
		process.stdout.write(hasOff + "," + hasOnce + "," + hasPause + "," + hasLC + "," + hasStdoutOnce);
	`)
	el.Run()
	if outBuf.String() != "true,true,true,true,true" {
		t.Errorf("expected all true, got '%s'", outBuf.String())
	}
}

func TestStdio_DataEventBuffer(t *testing.T) {
	// Verify stdin 'data' event passes Buffer objects (required by MCP SDK)
	vm := New()
	el := NewEventLoop(vm)
	RegisterBuffer(vm)

	var outBuf bytes.Buffer
	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader("hello\n"),
		Stdout: &outBuf,
		Stderr: &bytes.Buffer{},
	})

	vm.Run(`
		process.stdin.on("data", (chunk) => {
			const isB = Buffer.isBuffer(chunk);
			process.stdout.write("isBuffer:" + isB + " len:" + chunk.length + "\n");
		});
	`)

	el.Run()
	out := outBuf.String()
	if !strings.Contains(out, "isBuffer:true") {
		t.Errorf("expected Buffer chunk, got: %s", out)
	}
	if !strings.Contains(out, "len:6") {
		t.Errorf("expected length 6 (hello\\n), got: %s", out)
	}
}

func TestEventLoop_Enqueue(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)

	result := ""
	el.Ref()
	el.Enqueue(func() {
		result = "queued"
		el.Unref()
	})

	el.Run()

	if result != "queued" {
		t.Errorf("expected 'queued', got '%s'", result)
	}
}

func TestEventLoop_RefUnref(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)

	// Add a ref, loop should stay alive
	el.Ref()

	done := make(chan struct{})
	go func() {
		el.Run()
		close(done)
	}()

	// Should not exit yet
	select {
	case <-done:
		t.Fatal("loop should not have exited with refs > 0")
	case <-time.After(50 * time.Millisecond):
	}

	// Unref — should exit
	el.Unref()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("loop should have exited after unref")
		el.Stop()
	}
}

func TestStdio_ProcessObjectPreserved(t *testing.T) {
	// Ensure RegisterStdio doesn't overwrite existing process properties
	vm := New()
	el := NewEventLoop(vm)

	vm.SetValue("process", NewObj(map[string]*Value{
		"argv": NewArr([]*Value{NewStr("test")}),
		"env":  NewObj(map[string]*Value{"HOME": NewStr("/root")}),
	}))

	RegisterStdio(vm, el, &StdioConfig{
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	})

	// argv and env should still be there
	argv := vm.Get("process").Get("argv")
	if argv.Len() != 1 {
		t.Error("process.argv should be preserved")
	}
	env := vm.Get("process").Get("env").Get("HOME")
	if env.String() != "/root" {
		t.Error("process.env should be preserved")
	}
	// stdin/stdout/stderr should be added
	if vm.Get("process").Get("stdin").IsUndefined() {
		t.Error("process.stdin should exist")
	}
	if vm.Get("process").Get("stdout").IsUndefined() {
		t.Error("process.stdout should exist")
	}
}
