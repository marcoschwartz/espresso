package espresso

import (
	"encoding/json"
	"math"
	"math/rand"
	"strconv"
	"strings"
)

// registerBuiltinGlobals adds JS built-in objects (JSON, Math, Object, Array, etc.)
// to the scope so the bytecode VM can access them via opLoadVar.
// The interpreter handles these inline, but bytecode needs them in scope.
func registerBuiltinGlobals(scope map[string]*Value) {
	// JSON
	scope["JSON"] = newObj(map[string]*Value{
		"parse": NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return Undefined
			}
			return jsonToValue(json.RawMessage(args[0].toStr()))
		}),
		"stringify": NewNativeFunc(func(args []*Value) *Value {
			if len(args) == 0 {
				return Undefined
			}
			// Third arg: indent. Matches JS: a number N → N spaces, a string
			// → that string as the indent token. Replacer (2nd arg) ignored.
			indent := ""
			if len(args) >= 3 {
				switch args[2].typ {
				case TypeNumber:
					n := int(args[2].num)
					if n > 10 {
						n = 10
					}
					if n > 0 {
						indent = strings.Repeat(" ", n)
					}
				case TypeString:
					s := args[2].toStr()
					if len(s) > 10 {
						s = s[:10]
					}
					indent = s
				}
			}
			iface := valueToInterface(args[0])
			var b []byte
			if indent != "" {
				b, _ = json.MarshalIndent(iface, "", indent)
			} else {
				b, _ = json.Marshal(iface)
			}
			return newStr(string(b))
		}),
	})

	// Math
	scope["Math"] = newObj(map[string]*Value{
		"PI":    newNum(math.Pi),
		"E":     newNum(math.E),
		"LN2":   newNum(math.Ln2),
		"LN10":  newNum(math.Log(10)),
		"LOG2E": newNum(math.Log2E),
		"SQRT2": newNum(math.Sqrt2),
		"abs":   NewNativeFunc(func(a []*Value) *Value { return newNum(math.Abs(a[0].toNum())) }),
		"ceil":  NewNativeFunc(func(a []*Value) *Value { return newNum(math.Ceil(a[0].toNum())) }),
		"floor": NewNativeFunc(func(a []*Value) *Value { return newNum(math.Floor(a[0].toNum())) }),
		"round": NewNativeFunc(func(a []*Value) *Value { return newNum(math.Round(a[0].toNum())) }),
		"max": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 {
				return newNum(math.Inf(-1))
			}
			m := a[0].toNum()
			for _, v := range a[1:] {
				n := v.toNum()
				if n > m {
					m = n
				}
			}
			return newNum(m)
		}),
		"min": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 {
				return newNum(math.Inf(1))
			}
			m := a[0].toNum()
			for _, v := range a[1:] {
				n := v.toNum()
				if n < m {
					m = n
				}
			}
			return newNum(m)
		}),
		"pow":    NewNativeFunc(func(a []*Value) *Value { return newNum(math.Pow(a[0].toNum(), a[1].toNum())) }),
		"sqrt":   NewNativeFunc(func(a []*Value) *Value { return newNum(math.Sqrt(a[0].toNum())) }),
		"random": NewNativeFunc(func(a []*Value) *Value { return newNum(rand.Float64()) }),
		"log":    NewNativeFunc(func(a []*Value) *Value { return newNum(math.Log(a[0].toNum())) }),
		"log2":   NewNativeFunc(func(a []*Value) *Value { return newNum(math.Log2(a[0].toNum())) }),
		"log10":  NewNativeFunc(func(a []*Value) *Value { return newNum(math.Log10(a[0].toNum())) }),
		"sin":    NewNativeFunc(func(a []*Value) *Value { return newNum(math.Sin(a[0].toNum())) }),
		"cos":    NewNativeFunc(func(a []*Value) *Value { return newNum(math.Cos(a[0].toNum())) }),
		"tan":    NewNativeFunc(func(a []*Value) *Value { return newNum(math.Tan(a[0].toNum())) }),
		"trunc":  NewNativeFunc(func(a []*Value) *Value { return newNum(math.Trunc(a[0].toNum())) }),
		"sign": NewNativeFunc(func(a []*Value) *Value {
			n := a[0].toNum()
			if n > 0 {
				return newNum(1)
			} else if n < 0 {
				return newNum(-1)
			}
			return newNum(0)
		}),
	})

	// Object
	scope["Object"] = newObj(map[string]*Value{
		"keys": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 || a[0].typ != TypeObject || a[0].object == nil {
				return newArr(nil)
			}
			keys := make([]*Value, 0, len(a[0].object))
			for k := range a[0].object {
				keys = append(keys, newStr(k))
			}
			return &Value{typ: TypeArray, array: keys}
		}),
		"values": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 || a[0].typ != TypeObject || a[0].object == nil {
				return newArr(nil)
			}
			vals := make([]*Value, 0, len(a[0].object))
			for _, v := range a[0].object {
				vals = append(vals, v)
			}
			return &Value{typ: TypeArray, array: vals}
		}),
		"entries": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 || a[0].typ != TypeObject || a[0].object == nil {
				return newArr(nil)
			}
			entries := make([]*Value, 0, len(a[0].object))
			for k, v := range a[0].object {
				entries = append(entries, &Value{typ: TypeArray, array: []*Value{newStr(k), v}})
			}
			return &Value{typ: TypeArray, array: entries}
		}),
		"assign": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 {
				return Undefined
			}
			target := a[0]
			if target.typ != TypeObject || target.object == nil {
				return target
			}
			for _, src := range a[1:] {
				if src.typ == TypeObject && src.object != nil {
					for k, v := range src.object {
						target.object[k] = v
					}
				}
			}
			return target
		}),
		"freeze": NewNativeFunc(func(a []*Value) *Value {
			if len(a) > 0 {
				return a[0]
			}
			return Undefined
		}),
	})

	// Array
	scope["Array"] = newObj(map[string]*Value{
		"isArray": NewNativeFunc(func(a []*Value) *Value {
			if len(a) > 0 && a[0].typ == TypeArray {
				return True
			}
			return False
		}),
		"from": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 {
				return newArr(nil)
			}
			src := a[0]
			if src.typ == TypeArray {
				cp := make([]*Value, len(src.array))
				copy(cp, src.array)
				return &Value{typ: TypeArray, array: cp}
			}
			return newArr(nil)
		}),
	})

	// parseInt / parseFloat / isNaN / isFinite
	scope["parseInt"] = NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return newNum(math.NaN())
		}
		s := strings.TrimSpace(a[0].toStr())
		base := 10
		if len(a) > 1 {
			base = int(a[1].toNum())
		}
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			s = s[2:]
			if base == 10 {
				base = 16
			}
		}
		n, err := strconv.ParseInt(s, base, 64)
		if err != nil {
			return newNum(math.NaN())
		}
		return newNum(float64(n))
	})

	scope["parseFloat"] = NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return newNum(math.NaN())
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(a[0].toStr()), 64)
		if err != nil {
			return newNum(math.NaN())
		}
		return newNum(n)
	})

	scope["isNaN"] = NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return True
		}
		return newBool(math.IsNaN(a[0].toNum()))
	})

	scope["isFinite"] = NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return False
		}
		n := a[0].toNum()
		return newBool(!math.IsNaN(n) && !math.IsInf(n, 0))
	})

	numCtor := NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return newNum(0)
		}
		return newNum(a[0].toNum())
	})
	numCtor.object = map[string]*Value{
		"isInteger": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 {
				return False
			}
			n := a[0].toNum()
			return newBool(n == math.Trunc(n) && !math.IsInf(n, 0))
		}),
		"isFinite": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 {
				return False
			}
			n := a[0].toNum()
			return newBool(!math.IsNaN(n) && !math.IsInf(n, 0))
		}),
		"isNaN": NewNativeFunc(func(a []*Value) *Value {
			if len(a) == 0 {
				return False
			}
			return newBool(math.IsNaN(a[0].toNum()))
		}),
	}
	scope["Number"] = numCtor

	scope["String"] = NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return internStr("")
		}
		return newStr(a[0].toStr())
	})

	scope["Boolean"] = NewNativeFunc(func(a []*Value) *Value {
		if len(a) == 0 {
			return False
		}
		return newBool(a[0].truthy())
	})

	// console
	scope["console"] = newObj(map[string]*Value{
		"log":   NewNativeFunc(func(a []*Value) *Value { return Undefined }),
		"error": NewNativeFunc(func(a []*Value) *Value { return Undefined }),
		"warn":  NewNativeFunc(func(a []*Value) *Value { return Undefined }),
		"info":  NewNativeFunc(func(a []*Value) *Value { return Undefined }),
		"debug": NewNativeFunc(func(a []*Value) *Value { return Undefined }),
	})

	// Infinity, NaN
	scope["Infinity"] = newNum(math.Inf(1))
	scope["NaN"] = newNum(math.NaN())
}
