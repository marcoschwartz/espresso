package espresso

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// ─── Buffer ─────────────────────────────────────────────
// Node.js-compatible Buffer for binary data handling.
// Stores data as a Go []byte, exposed to JS with standard methods.

type jsBuffer struct {
	data []byte
}

// RegisterBuffer adds the global Buffer object to the VM scope.
func RegisterBuffer(vm *VM) {
	bufferObj := NewObj(map[string]*Value{
		// Buffer.from(input, encoding?)
		"from": NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return newBufferValue(nil)
			}
			first := args[0]

			// Buffer.from(string, encoding?)
			if first.typ == TypeString {
				encoding := "utf8"
				if len(args) > 1 {
					encoding = args[1].toStr()
				}
				data := decodeString(first.str, encoding)
				return newBufferValue(data)
			}

			// Buffer.from(array)
			if first.typ == TypeArray {
				data := make([]byte, len(first.array))
				for i, v := range first.array {
					data[i] = byte(int(v.toNum()) & 0xFF)
				}
				return newBufferValue(data)
			}

			// Buffer.from(buffer) — copy
			if first.typ == TypeObject {
				if buf := getBuffer(first); buf != nil {
					cp := make([]byte, len(buf.data))
					copy(cp, buf.data)
					return newBufferValue(cp)
				}
			}

			return newBufferValue(nil)
		}),

		// Buffer.alloc(size, fill?)
		"alloc": NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return newBufferValue(nil)
			}
			size := int(args[0].toNum())
			if size < 0 {
				size = 0
			}
			data := make([]byte, size)
			if len(args) > 1 {
				fill := byte(int(args[1].toNum()) & 0xFF)
				for i := range data {
					data[i] = fill
				}
			}
			return newBufferValue(data)
		}),

		// Buffer.isBuffer(obj)
		"isBuffer": NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return False
			}
			return newBool(getBuffer(args[0]) != nil)
		}),

		// Buffer.concat(list, totalLength?)
		"concat": NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 || args[0].typ != TypeArray {
				return newBufferValue(nil)
			}
			var result []byte
			for _, item := range args[0].array {
				if buf := getBuffer(item); buf != nil {
					result = append(result, buf.data...)
				}
			}
			return newBufferValue(result)
		}),

		// Buffer.byteLength(string, encoding?)
		"byteLength": NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return newNum(0)
			}
			if args[0].typ == TypeString {
				encoding := "utf8"
				if len(args) > 1 {
					encoding = args[1].toStr()
				}
				data := decodeString(args[0].str, encoding)
				return newNum(float64(len(data)))
			}
			if buf := getBuffer(args[0]); buf != nil {
				return newNum(float64(len(buf.data)))
			}
			return newNum(0)
		}),
	})

	vm.SetValue("Buffer", bufferObj)
}

func newBufferValue(data []byte) *Value {
	if data == nil {
		data = []byte{}
	}
	buf := &jsBuffer{data: data}
	v := &Value{typ: TypeObject, object: make(map[string]*Value), Custom: buf}
	v.object["__constructor__"] = newStr("Buffer")

	// length
	v.DefineGetter("length", func(args []*Value) *Value {
		return newNum(float64(len(buf.data)))
	})

	// toString(encoding?, start?, end?)
	v.object["toString"] = NewNativeFunc(func(args []*Value) *Value {
		encoding := "utf8"
		start := 0
		end := len(buf.data)
		if len(args) > 0 && args[0].typ == TypeString {
			encoding = args[0].toStr()
		}
		if len(args) > 1 {
			start = int(args[1].toNum())
			if start < 0 { start = 0 }
			if start > len(buf.data) { start = len(buf.data) }
		}
		if len(args) > 2 {
			end = int(args[2].toNum())
			if end < 0 { end = 0 }
			if end > len(buf.data) { end = len(buf.data) }
		}
		return newStr(encodeString(buf.data[start:end], encoding))
	})

	// toJSON()
	v.object["toJSON"] = NewNativeFunc(func(args []*Value) *Value {
		arr := make([]*Value, len(buf.data))
		for i, b := range buf.data {
			arr[i] = newNum(float64(b))
		}
		return NewObj(map[string]*Value{
			"type": newStr("Buffer"),
			"data": newArr(arr),
		})
	})

	// slice(start?, end?)
	v.object["slice"] = NewNativeFunc(func(args []*Value) *Value {
		start := 0
		end := len(buf.data)
		if len(args) > 0 {
			start = int(args[0].toNum())
			if start < 0 { start = len(buf.data) + start }
			if start < 0 { start = 0 }
		}
		if len(args) > 1 {
			end = int(args[1].toNum())
			if end < 0 { end = len(buf.data) + end }
			if end < 0 { end = 0 }
		}
		if start > len(buf.data) { start = len(buf.data) }
		if end > len(buf.data) { end = len(buf.data) }
		if start > end { start = end }
		cp := make([]byte, end-start)
		copy(cp, buf.data[start:end])
		return newBufferValue(cp)
	})

	// write(string, offset?, length?, encoding?)
	v.object["write"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return newNum(0) }
		str := args[0].toStr()
		offset := 0
		if len(args) > 1 { offset = int(args[1].toNum()) }
		encoding := "utf8"
		if len(args) > 3 { encoding = args[3].toStr() }
		src := decodeString(str, encoding)
		maxLen := len(buf.data) - offset
		if len(args) > 2 {
			ml := int(args[2].toNum())
			if ml < maxLen { maxLen = ml }
		}
		if maxLen < 0 { maxLen = 0 }
		if len(src) > maxLen { src = src[:maxLen] }
		copy(buf.data[offset:], src)
		return newNum(float64(len(src)))
	})

	// equals(other)
	v.object["equals"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return False }
		other := getBuffer(args[0])
		if other == nil { return False }
		if len(buf.data) != len(other.data) { return False }
		for i := range buf.data {
			if buf.data[i] != other.data[i] { return False }
		}
		return True
	})

	// copy(target, targetStart?, sourceStart?, sourceEnd?)
	v.object["copy"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return newNum(0) }
		target := getBuffer(args[0])
		if target == nil { return newNum(0) }
		tStart := 0
		sStart := 0
		sEnd := len(buf.data)
		if len(args) > 1 { tStart = int(args[1].toNum()) }
		if len(args) > 2 { sStart = int(args[2].toNum()) }
		if len(args) > 3 { sEnd = int(args[3].toNum()) }
		n := copy(target.data[tStart:], buf.data[sStart:sEnd])
		return newNum(float64(n))
	})

	// Numeric index access via getter
	v.DefineGetter("__index__", func(args []*Value) *Value {
		// This is a fallback — individual indexes are set below
		return Undefined
	})

	// subarray(start?, end?) — returns a new Buffer sharing the same memory
	v.object["subarray"] = NewNativeFunc(func(args []*Value) *Value {
		start := 0
		end := len(buf.data)
		if len(args) > 0 {
			start = int(args[0].toNum())
			if start < 0 { start = len(buf.data) + start }
			if start < 0 { start = 0 }
		}
		if len(args) > 1 {
			end = int(args[1].toNum())
			if end < 0 { end = len(buf.data) + end }
			if end < 0 { end = 0 }
		}
		if start > len(buf.data) { start = len(buf.data) }
		if end > len(buf.data) { end = len(buf.data) }
		if start > end { start = end }
		return newBufferValue(buf.data[start:end])
	})

	// indexOf(value, byteOffset?) — find position of value in buffer
	v.object["indexOf"] = NewNativeFunc(func(args []*Value) *Value {
		if len(args) == 0 { return newNum(-1) }
		offset := 0
		if len(args) > 1 { offset = int(args[1].toNum()) }
		if offset < 0 { offset = 0 }
		if offset >= len(buf.data) { return newNum(-1) }
		search := buf.data[offset:]
		// Search for string or byte
		val := args[0]
		if val.typ == TypeString {
			needle := []byte(val.str)
			for i := 0; i <= len(search)-len(needle); i++ {
				match := true
				for j := range needle {
					if search[i+j] != needle[j] { match = false; break }
				}
				if match { return newNum(float64(offset + i)) }
			}
			return newNum(-1)
		}
		if val.typ == TypeNumber {
			b := byte(int(val.num) & 0xFF)
			for i, c := range search {
				if c == b { return newNum(float64(offset + i)) }
			}
			return newNum(-1)
		}
		return newNum(-1)
	})

	return v
}

func getBuffer(v *Value) *jsBuffer {
	if v == nil || v.typ != TypeObject {
		return nil
	}
	if buf, ok := v.Custom.(*jsBuffer); ok {
		return buf
	}
	return nil
}

func decodeString(s, encoding string) []byte {
	switch strings.ToLower(encoding) {
	case "base64":
		data, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			data, _ = base64.RawStdEncoding.DecodeString(s)
		}
		return data
	case "hex":
		data, _ := hex.DecodeString(s)
		return data
	default: // utf8, utf-8, ascii, latin1, binary
		return []byte(s)
	}
}

func encodeString(data []byte, encoding string) string {
	switch strings.ToLower(encoding) {
	case "base64":
		return base64.StdEncoding.EncodeToString(data)
	case "hex":
		return hex.EncodeToString(data)
	default:
		return string(data)
	}
}
