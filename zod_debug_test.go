package espresso

import (
	"fmt"
	"testing"
)

func TestZodRealRequire(t *testing.T) {
	RequireDebug = false

	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	ms := NewModuleSystem(vm, "/root/code/frontends/lungo/espresso")
	ms.RegisterGlobals("")

	// Test like the CLI does: vm.Run with require inside JS
	vm.Run(`
		var zod = require("zod");
		var zodReqType = typeof zod.string;
		var zodReqKeys = Object.keys(zod).length;
	`)
	zodReqType := vm.Get("zodReqType")
	zodReqKeys := vm.Get("zodReqKeys")
	fmt.Printf("JS typeof require('zod').string = %q\n", zodReqType.String())
	fmt.Printf("JS Object.keys(require('zod')).length = %v\n", zodReqKeys.Number())

	// Also check the zod object directly
	zod := vm.Get("zod")
	fmt.Printf("Go zod.typ=%d keys=%d\n", zod.typ, len(zod.object))
	if strV, ok := zod.object["string"]; ok {
		fmt.Printf("Go zod.object[string] typ=%d str=%q\n", strV.typ, strV.str)
	} else {
		fmt.Println("Go zod.object[string] MISSING")
	}
	fmt.Printf("Go getProp('string') typ=%d\n", zod.getProp("string").typ)
}

func TestZodGetterDebug(t *testing.T) {
	vm := New()

	vm.Run(`
		var source = { hello: "world" };
		var obj = {};
		Object.defineProperty(obj, "test", {
			enumerable: true,
			get: function() { return source.hello; }
		});
	`)

	obj := vm.Get("obj")
	fmt.Printf("obj type: %d\n", obj.typ)

	if obj.getset != nil {
		for k, pd := range obj.getset {
			fmt.Printf("getset[%s]: Get.typ=%d Get.str=%q Get.native=%v Get.fnBody=%q\n",
				k, pd.Get.typ, pd.Get.str, pd.Get.native != nil, pd.Get.fnBody)
		}
	} else {
		fmt.Println("getset is nil!")
	}

	val := obj.getProp("test")
	fmt.Printf("obj.test = %q (type=%d)\n", val.String(), val.typ)
}

func TestZodCrossModuleGetter(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	exports := NewObj(make(map[string]*Value))

	childVM := New()
	for k, v := range vm.scope {
		childVM.scope[k] = v
	}
	childVM.scope["exports"] = exports

	childVM.Run(`
		var inner = { hello: function() { return "world"; }, name: "test" };
		Object.defineProperty(exports, "hello", {
			enumerable: true,
			get: function() { return inner.hello; }
		});
		Object.defineProperty(exports, "name", {
			enumerable: true,
			get: function() { return inner.name; }
		});
	`)

	fmt.Printf("\n--- After child VM ---\n")
	if exports.getset != nil {
		for k, pd := range exports.getset {
			if pd.Get != nil {
				fmt.Printf("getset[%s]: typ=%d str=%q native=%v fnBody=%q num=%v\n",
					k, pd.Get.typ, pd.Get.str, pd.Get.native != nil, pd.Get.fnBody, pd.Get.num)
				// Inspect the arrow's captured scope
				arrowID := int(pd.Get.num)
				arrowRegistryMu.Lock()
				af, ok := arrowRegistry[arrowID]
				arrowRegistryMu.Unlock()
				if ok {
					fmt.Printf("  arrow scope keys: ")
					for sk := range af.scope {
						fmt.Printf("%s ", sk)
					}
					fmt.Println()
					if inner, exists := af.scope["inner"]; exists {
						fmt.Printf("  inner typ=%d\n", inner.typ)
						if inner.object != nil {
							for ik, iv := range inner.object {
								fmt.Printf("    inner.%s typ=%d str=%q\n", ik, iv.typ, iv.str)
							}
						}
					} else {
						fmt.Println("  inner NOT in scope!")
					}
					fmt.Printf("  arrow tokens: ")
					for _, tok := range af.tokens {
						fmt.Printf("%s ", tok.v)
					}
					fmt.Println()
				}
			}
		}
	}

	val := exports.getProp("hello")
	fmt.Printf("exports.hello = %q (type=%d)\n", val.String(), val.typ)

	val2 := exports.getProp("name")
	fmt.Printf("exports.name = %q (type=%d)\n", val2.String(), val2.typ)
}
