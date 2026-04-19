package espresso

import "testing"

// ─── Buffer Tests ───────────────────────────────────────

func newVMWithBuffer() *VM {
	vm := New()
	RegisterBuffer(vm)
	return vm
}

func TestBuffer_FromString(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("hello");
		return buf.toString();
	`)
	if r.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", r.String())
	}
}

func TestBuffer_FromStringUTF8(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("hello", "utf8");
		return buf.length;
	`)
	if r.Number() != 5 {
		t.Errorf("expected length 5, got %v", r.Number())
	}
}

func TestBuffer_FromBase64(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("aGVsbG8=", "base64");
		return buf.toString();
	`)
	if r.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", r.String())
	}
}

func TestBuffer_ToBase64(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("hello");
		return buf.toString("base64");
	`)
	if r.String() != "aGVsbG8=" {
		t.Errorf("expected 'aGVsbG8=', got '%s'", r.String())
	}
}

func TestBuffer_FromHex(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("48656c6c6f", "hex");
		return buf.toString();
	`)
	if r.String() != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", r.String())
	}
}

func TestBuffer_ToHex(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("Hello");
		return buf.toString("hex");
	`)
	if r.String() != "48656c6c6f" {
		t.Errorf("expected '48656c6c6f', got '%s'", r.String())
	}
}

func TestBuffer_Alloc(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.alloc(4);
		return buf.length;
	`)
	if r.Number() != 4 {
		t.Errorf("expected length 4, got %v", r.Number())
	}
}

func TestBuffer_AllocFill(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.alloc(3, 65);
		return buf.toString();
	`)
	if r.String() != "AAA" {
		t.Errorf("expected 'AAA', got '%s'", r.String())
	}
}

func TestBuffer_IsBuffer(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("test");
		const notBuf = { length: 4 };
		return String(Buffer.isBuffer(buf)) + "," + String(Buffer.isBuffer(notBuf));
	`)
	if r.String() != "true,false" {
		t.Errorf("expected 'true,false', got '%s'", r.String())
	}
}

func TestBuffer_Slice(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("hello world");
		const sliced = buf.slice(0, 5);
		return sliced.toString();
	`)
	if r.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", r.String())
	}
}

func TestBuffer_SliceNegative(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("hello world");
		return buf.slice(-5).toString();
	`)
	if r.String() != "world" {
		t.Errorf("expected 'world', got '%s'", r.String())
	}
}

func TestBuffer_Concat(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const a = Buffer.from("hello ");
		const b = Buffer.from("world");
		const c = Buffer.concat([a, b]);
		return c.toString();
	`)
	if r.String() != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", r.String())
	}
}

func TestBuffer_Equals(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const a = Buffer.from("hello");
		const b = Buffer.from("hello");
		const c = Buffer.from("world");
		return String(a.equals(b)) + "," + String(a.equals(c));
	`)
	if r.String() != "true,false" {
		t.Errorf("expected 'true,false', got '%s'", r.String())
	}
}

func TestBuffer_ByteLength(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		return Buffer.byteLength("hello");
	`)
	if r.Number() != 5 {
		t.Errorf("expected 5, got %v", r.Number())
	}
}

func TestBuffer_FromArray(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from([72, 101, 108, 108, 111]);
		return buf.toString();
	`)
	if r.String() != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", r.String())
	}
}

func TestBuffer_Write(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.alloc(5);
		buf.write("Hi");
		return buf.toString().slice(0, 2);
	`)
	if r.String() != "Hi" {
		t.Errorf("expected 'Hi', got '%s'", r.String())
	}
}

func TestBuffer_ToJSON(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const buf = Buffer.from("AB");
		const j = buf.toJSON();
		return j.type + ":" + j.data.length;
	`)
	if r.String() != "Buffer:2" {
		t.Errorf("expected 'Buffer:2', got '%s'", r.String())
	}
}

func TestBuffer_IndexOf(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const b = Buffer.from("hello\\nworld");
		return b.indexOf("\\n");
	`)
	if r.Number() != 5 { t.Errorf("expected 5, got %v", r.Number()) }
}

func TestBuffer_Subarray(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const b = Buffer.from("hello world");
		return b.subarray(6).toString();
	`)
	if r.String() != "world" { t.Errorf("expected 'world', got '%s'", r.String()) }
}

func TestBuffer_ToStringRange(t *testing.T) {
	vm := newVMWithBuffer()
	r, _ := vm.Run(`
		const b = Buffer.from("hello world");
		return b.toString("utf8", 0, 5);
	`)
	if r.String() != "hello" { t.Errorf("expected 'hello', got '%s'", r.String()) }
}
