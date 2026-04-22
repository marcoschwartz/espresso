// Package espresso is a lightweight JavaScript evaluator written in pure Go.
// It supports most common JS patterns: variables, operators, functions, arrows,
// closures, array/string/object methods, template literals, loops, try/catch,
// and more — without any external dependencies.
package espresso

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Type represents a JavaScript value type.
type Type int

const (
	TypeUndefined Type = iota
	TypeNull
	TypeBool
	TypeNumber
	TypeString
	TypeArray
	TypeObject
	TypeFunc
	TypeCustom // for embedding Go objects (e.g. SSR vnodes)
)

// NativeFunc is a Go function callable from JS.
type NativeFunc func(args []*Value) *Value

// PropDescriptor holds a getter/setter pair for a property.
type PropDescriptor struct {
	Get *Value // getter function
	Set *Value // setter function
}

// Value represents a JavaScript value.
type Value struct {
	typ      Type
	bool     bool
	num      float64
	str      string
	array    []*Value
	object   map[string]*Value
	getset   map[string]*PropDescriptor // getter/setter descriptors
	proto    *Value                     // prototype chain
	fnParams []string
	fnBody   string
	fnScope  map[string]*Value // captured scope for module-exported fnBody functions
	native   NativeFunc        // for Go-native functions
	Custom   interface{} // for embedding Go objects (e.g. SSR vnodes)
	bc       *bytecode   // cached bytecode for function bodies
}

// Undefined is the JS undefined value.
var Undefined = &Value{typ: TypeUndefined}

// Break and Continue are sentinel values for loop control flow.
var breakSentinel = &Value{typ: TypeUndefined, str: "__break__"}
var continueSentinel = &Value{typ: TypeUndefined, str: "__continue__"}

// throwSentinel wraps a thrown value. The thrown value is stored in object["__thrown__"].
func newThrow(val *Value) *Value {
	return &Value{typ: TypeUndefined, str: "__throw__", object: map[string]*Value{"__thrown__": val}}
}

// isThrow checks if a value is a throw sentinel.
func isThrow(v *Value) bool {
	return v != nil && v.str == "__throw__" && v.object != nil
}

// thrownValue extracts the thrown value from a throw sentinel.
func thrownValue(v *Value) *Value {
	if v != nil && v.object != nil {
		if tv, ok := v.object["__thrown__"]; ok {
			return tv
		}
	}
	return Undefined
}

// DefineGetter defines a getter on an object property (Go API for embedders).
func (v *Value) DefineGetter(prop string, fn NativeFunc) {
	if v.getset == nil {
		v.getset = make(map[string]*PropDescriptor)
	}
	desc, ok := v.getset[prop]
	if !ok {
		desc = &PropDescriptor{}
		v.getset[prop] = desc
	}
	desc.Get = NewNativeFunc(fn)
}

// DefineSetter defines a setter on an object property (Go API for embedders).
func (v *Value) DefineSetter(prop string, fn NativeFunc) {
	if v.getset == nil {
		v.getset = make(map[string]*PropDescriptor)
	}
	desc, ok := v.getset[prop]
	if !ok {
		desc = &PropDescriptor{}
		v.getset[prop] = desc
	}
	desc.Set = NewNativeFunc(fn)
}

// newError creates a JS Error object with name and message properties.
func newError(name, message string) *Value {
	return newObj(map[string]*Value{
		"name":    newStr(name),
		"message": newStr(message),
		"stack":   newStr(name + ": " + message),
	})
}

// Null is the JS null value.
var Null = &Value{typ: TypeNull}

// True is the JS true value.
var True = &Value{typ: TypeBool, bool: true}

// False is the JS false value.
var False = &Value{typ: TypeBool, bool: false}


// String returns the string representation of the value.
func (v *Value) String() string {
	if v == nil {
		return "undefined"
	}
	return v.toStr()
}

// Number returns the numeric value.
func (v *Value) Number() float64 {
	if v == nil {
		return 0
	}
	return v.toNum()
}

// Bool returns the boolean value.
func (v *Value) Bool() bool {
	if v == nil {
		return false
	}
	return v.bool
}

// Type returns the value's type.
func (v *Value) Type() Type {
	if v == nil {
		return TypeUndefined
	}
	return v.typ
}

// IsNull returns true if the value is null.
func (v *Value) IsNull() bool { return v != nil && v.typ == TypeNull }

// IsUndefined returns true if the value is undefined.
func (v *Value) IsUndefined() bool { return v == nil || v.typ == TypeUndefined }

// IsArray returns true if the value is an array.
func (v *Value) IsArray() bool { return v != nil && v.typ == TypeArray }

// IsObject returns true if the value is an object.
func (v *Value) IsObject() bool { return v != nil && v.typ == TypeObject }

// Truthy returns the JS truthiness of the value.
func (v *Value) Truthy() bool {
	if v == nil {
		return false
	}
	return v.truthy()
}

// Get returns a property of an object or array element.
func (v *Value) Get(key string) *Value {
	if v == nil {
		return Undefined
	}
	return v.getProp(key)
}

// Array returns the array elements as a slice.
func (v *Value) Array() []*Value {
	if v == nil || v.typ != TypeArray {
		return nil
	}
	return v.array
}

// Object returns the object properties as a map.
func (v *Value) Object() map[string]*Value {
	if v == nil || v.typ != TypeObject {
		return nil
	}
	return v.object
}

// FnParams returns the function's parameter specs as stored on the Value.
// Each entry may be a comma-separated list (e.g. "a,b,c") or a destructuring
// pattern ("{a, b}"). Returns nil for non-function values.
func (v *Value) FnParams() []string {
	if v == nil || v.typ != TypeFunc {
		return nil
	}
	return v.fnParams
}

// Len returns the length of an array or string.
func (v *Value) Len() int {
	if v == nil {
		return 0
	}
	if v.typ == TypeArray {
		return len(v.array)
	}
	if v.typ == TypeString {
		return len(v.str)
	}
	return 0
}

// Interface converts the Value to a native Go type.
func (v *Value) Interface() interface{} {
	return valueToInterface(v)
}

// ── Internal helpers ────────────────────────────────────

func (v *Value) truthy() bool {
	switch v.typ {
	case TypeUndefined, TypeNull:
		return false
	case TypeBool:
		return v.bool
	case TypeNumber:
		return v.num != 0
	case TypeString:
		return v.str != ""
	case TypeArray, TypeObject, TypeFunc, TypeCustom:
		return true
	}
	return false
}

func (v *Value) toStr() string {
	switch v.typ {
	case TypeUndefined:
		return "undefined"
	case TypeNull:
		return "null"
	case TypeBool:
		if v.bool {
			return "true"
		}
		return "false"
	case TypeNumber:
		if v.num == float64(int64(v.num)) {
			return strconv.FormatInt(int64(v.num), 10)
		}
		return strconv.FormatFloat(v.num, 'f', -1, 64)
	case TypeString:
		return v.str
	case TypeArray:
		var parts []string
		for _, item := range v.array {
			parts = append(parts, item.toStr())
		}
		return strings.Join(parts, ",")
	case TypeObject:
		b, _ := json.Marshal(valueToInterface(v))
		return string(b)
	}
	return ""
}

func (v *Value) toNum() float64 {
	switch v.typ {
	case TypeUndefined:
		return 0
	case TypeNull:
		return 0
	case TypeBool:
		if v.bool {
			return 1
		}
		return 0
	case TypeNumber:
		return v.num
	case TypeString:
		n, err := strconv.ParseFloat(strings.TrimSpace(v.str), 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func (v *Value) getProp(key string) *Value {
	// TypeFunc with object map — used for class constructors, module exports
	if v.typ == TypeFunc && v.object != nil {
		if val, ok := v.object[key]; ok {
			return val
		}
	}
	if v.typ == TypeObject {
		// Check getters on this object and prototype chain, binding this to the original object
		for cur := v; cur != nil; cur = cur.proto {
			if cur.getset != nil {
				if desc, ok := cur.getset[key]; ok && desc.Get != nil {
					if desc.Get.native != nil {
						return desc.Get.native(nil)
					}
					if desc.Get.str == "__arrow" {
						scope := map[string]*Value{"this": v}
						return callArrow(int(desc.Get.num), nil, scope)
					}
				}
			}
		}
		if v.object != nil {
			// Proxy get trap — intercept ALL property reads (known and unknown)
			if _, isProxy := v.object["__proxy_get__"]; isProxy {
				if key != "__proxy_get__" && key != "__proxy__" && key != "__has__" && key != "__delete__" && key != "__apply__" && key != "__constructor__" && key != "__constructors__" {
					if getTrap, ok := v.object["__proxy_get__"]; ok && getTrap.typ == TypeFunc && getTrap.native != nil {
						return getTrap.native([]*Value{newStr(key)})
					}
				}
			}
			if val, ok := v.object[key]; ok {
				return val
			}
		}
		// Check prototype chain
		if v.proto != nil {
			return v.proto.getProp(key)
		}
		return Undefined
	}
	if v.typ == TypeArray {
		if key == "length" {
			return newNum(float64(len(v.array)))
		}
		idx, err := strconv.Atoi(key)
		if err == nil && idx >= 0 && idx < len(v.array) {
			return v.array[idx]
		}
		return Undefined
	}
	if v.typ == TypeString {
		if key == "length" {
			return newNum(float64(len(v.str)))
		}
		idx, err := strconv.Atoi(key)
		if err == nil && idx >= 0 && idx < len(v.str) {
			return newStr(string(v.str[idx]))
		}
		return Undefined
	}
	// Function properties (static methods, __class__, __prototype__, length, etc.)
	if v.typ == TypeFunc {
		if key == "length" {
			// Return param count for arrow functions
			if v.str == "__arrow" {
				arrowRegistryMu.Lock()
				af, ok := arrowRegistry[int(v.num)]
				arrowRegistryMu.Unlock()
				if ok {
					count := 0
					for _, p := range af.params {
						if !strings.HasPrefix(p, "__rest__:") {
							count++
						}
					}
					return newNum(float64(count))
				}
			}
			if v.fnParams != nil {
				// Old-style function: params might be comma-separated in one string
				if len(v.fnParams) == 1 && strings.Contains(v.fnParams[0], ",") {
					return newNum(float64(len(strings.Split(v.fnParams[0], ","))))
				}
				return newNum(float64(len(v.fnParams)))
			}
			return newNum(0)
		}
		if v.object != nil {
			if val, ok := v.object[key]; ok {
				return val
			}
		}
	}
	return Undefined
}

// ── Constructors ────────────────────────────────────────

func newStr(s string) *Value     { return internStr(s) }
func newNum(n float64) *Value    { return internNum(n) }
func newBool(b bool) *Value      { if b { return True }; return False }
func newArr(a []*Value) *Value   { return &Value{typ: TypeArray, array: a} }
func newObj(o map[string]*Value) *Value { return &Value{typ: TypeObject, object: o} }

// NewCustom creates a Value that wraps an arbitrary Go object.
func NewCustom(v interface{}) *Value { return &Value{typ: TypeCustom, Custom: v} }

// NewNativeFunc creates a Value wrapping a Go function callable from JS.
func NewNativeFunc(fn NativeFunc) *Value { return &Value{typ: TypeFunc, native: fn} }

// NewStr creates a string Value (exported constructor).
func NewStr(s string) *Value { return newStr(s) }

// NewNum creates a number Value (exported constructor).
func NewNum(n float64) *Value { return newNum(n) }

// NewBool creates a boolean Value (exported constructor).
func NewBool(b bool) *Value { return newBool(b) }

// NewArr creates an array Value (exported constructor).
func NewArr(a []*Value) *Value { return newArr(a) }

// NewObj creates an object Value (exported constructor).
func NewObj(o map[string]*Value) *Value { return newObj(o) }

// IsCustom returns true if the value is a custom Go object.
func (v *Value) IsCustom() bool { return v != nil && v.typ == TypeCustom }

// looseEqual implements JS == with type coercion.
func looseEqual(a, b *Value) bool {
	if a.typ == b.typ {
		return strictEqual(a, b)
	}
	// null == undefined
	if (a.typ == TypeNull && b.typ == TypeUndefined) || (a.typ == TypeUndefined && b.typ == TypeNull) {
		return true
	}
	// number == string → compare as numbers
	if a.typ == TypeNumber && b.typ == TypeString {
		return a.num == b.toNum()
	}
	if a.typ == TypeString && b.typ == TypeNumber {
		return a.toNum() == b.num
	}
	// bool == anything → convert bool to number
	if a.typ == TypeBool {
		return looseEqual(newNum(a.toNum()), b)
	}
	if b.typ == TypeBool {
		return looseEqual(a, newNum(b.toNum()))
	}
	return false
}

func strictEqual(a, b *Value) bool {
	if a.typ != b.typ {
		return false
	}
	switch a.typ {
	case TypeUndefined, TypeNull:
		return true
	case TypeBool:
		return a.bool == b.bool
	case TypeNumber:
		return a.num == b.num
	case TypeString:
		return a.str == b.str
	}
	return a == b // reference equality for objects/arrays
}

func valueToInterface(v *Value) interface{} {
	if v == nil {
		return nil
	}
	switch v.typ {
	case TypeUndefined, TypeNull:
		return nil
	case TypeBool:
		return v.bool
	case TypeNumber:
		return v.num
	case TypeString:
		return v.str
	case TypeArray:
		arr := make([]interface{}, len(v.array))
		for i, item := range v.array {
			arr[i] = valueToInterface(item)
		}
		return arr
	case TypeObject:
		obj := make(map[string]interface{}, len(v.object))
		for k, val := range v.object {
			obj[k] = valueToInterface(val)
		}
		return obj
	}
	return nil
}

// JsonToValue converts a JSON raw message to a Value.
func JsonToValue(data json.RawMessage) *Value {
	if data == nil {
		return Null
	}
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Null
	}
	return ToValue(raw)
}

// ToValue converts a Go value to a JS Value.
func ToValue(v interface{}) *Value {
	if v == nil {
		return Null
	}
	switch val := v.(type) {
	case bool:
		return newBool(val)
	case int:
		return newNum(float64(val))
	case int64:
		return newNum(float64(val))
	case float64:
		return newNum(val)
	case string:
		return newStr(val)
	case []interface{}:
		arr := make([]*Value, len(val))
		for i, item := range val {
			arr[i] = ToValue(item)
		}
		return newArr(arr)
	case map[string]interface{}:
		obj := make(map[string]*Value, len(val))
		for k, item := range val {
			obj[k] = ToValue(item)
		}
		return newObj(obj)
	case json.RawMessage:
		var raw interface{}
		json.Unmarshal(val, &raw)
		return ToValue(raw)
	case *Value:
		return val
	}
	return Undefined
}
