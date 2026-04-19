package espresso

import (
	"path/filepath"
	"runtime"
	"testing"
)

func mcpTestDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Dir(f)
}

// TestMCP_ForOfMap tests for...of iteration over Map objects.
func TestMCP_ForOfMap(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()
	_, err := vm.Run(`
		var m = new Map();
		m.set("a", 1);
		m.set("b", 2);
		var keys = "";
		var vals = 0;
		for (var entry of m) {
			keys += entry[0];
			vals += entry[1];
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	if k := vm.Get("keys"); k.String() != "ab" {
		t.Errorf("expected keys='ab', got '%s'", k.String())
	}
	if v := vm.Get("vals"); v.Number() != 3 {
		t.Errorf("expected vals=3, got %v", v.Number())
	}
}

// TestMCP_ForOfSet tests for...of iteration over Set objects.
func TestMCP_ForOfSet(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()
	_, err := vm.Run(`
		var s = new Set([10, 20, 30]);
		var sum = 0;
		for (var item of s) {
			sum += item;
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("sum"); v.Number() != 60 {
		t.Errorf("expected sum=60, got %v", v.Number())
	}
}

// TestMCP_ArrayRestDestructuring tests const [a, ...rest] = arr.
func TestMCP_ArrayRestDestructuring(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		const arr = [1, 2, 3, 4];
		const [first, ...rest] = arr;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("first"); v.Number() != 1 {
		t.Errorf("expected first=1, got %v", v.Number())
	}
	rest := vm.Get("rest")
	if rest.typ != TypeArray {
		t.Fatalf("expected rest to be array, got type %d", rest.typ)
	}
	if len(rest.array) != 3 {
		t.Fatalf("expected rest length=3, got %d", len(rest.array))
	}
	if rest.array[0].Number() != 2 {
		t.Errorf("expected rest[0]=2, got %v", rest.array[0].Number())
	}
	if rest.array[2].Number() != 4 {
		t.Errorf("expected rest[2]=4, got %v", rest.array[2].Number())
	}
}

// TestMCP_ArrayRestDestructuringEmpty tests rest is empty when array is short.
func TestMCP_ArrayRestDestructuringEmpty(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		const [a, ...rest] = [1];
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("a"); v.Number() != 1 {
		t.Errorf("expected a=1, got %v", v.Number())
	}
	rest := vm.Get("rest")
	if rest.typ != TypeArray {
		t.Fatalf("expected rest to be array, got type %d", rest.typ)
	}
	if len(rest.array) != 0 {
		t.Errorf("expected rest length=0, got %d", len(rest.array))
	}
}

// TestMCP_ArrayFindWithFunction tests array.find with function() callback.
func TestMCP_ArrayFindWithFunction(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		var found = [1, 2, 3].find(function(x) { return x > 1; });
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("found"); v.Number() != 2 {
		t.Errorf("expected found=2, got %v", v.Number())
	}
}

// TestMCP_ArrayFindWithArrow tests array.find with arrow callback.
func TestMCP_ArrayFindWithArrow(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		var found = [10, 20, 30].find(x => x > 15);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("found"); v.Number() != 20 {
		t.Errorf("expected found=20, got %v", v.Number())
	}
}

// TestMCP_ArrayFindNoMatch tests array.find returns undefined when no match.
func TestMCP_ArrayFindNoMatch(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		var found = [1, 2, 3].find(x => x > 100);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("found"); !v.IsUndefined() {
		t.Errorf("expected undefined, got %v", v.String())
	}
}

// TestMCP_ArraySpread tests [...arr, extra] syntax.
func TestMCP_ArraySpread(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		var a = [1, 2, 3];
		var b = [...a, 4, 5];
	`)
	if err != nil {
		t.Fatal(err)
	}
	b := vm.Get("b")
	if b.typ != TypeArray {
		t.Fatalf("expected array, got type %d", b.typ)
	}
	if len(b.array) != 5 {
		t.Fatalf("expected length=5, got %d", len(b.array))
	}
	for i, expected := range []float64{1, 2, 3, 4, 5} {
		if b.array[i].Number() != expected {
			t.Errorf("b[%d]: expected %v, got %v", i, expected, b.array[i].Number())
		}
	}
}

// TestMCP_SpreadInFunctionCall tests fn(...args) syntax.
func TestMCP_SpreadInFunctionCall(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		function add(a, b, c) { return a + b + c; }
		var args = [10, 20, 30];
		var result = add(...args);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("result"); v.Number() != 60 {
		t.Errorf("expected result=60, got %v", v.Number())
	}
}

// TestMCP_ClassStaticMethods tests static method calls on classes.
func TestMCP_ClassStaticMethods(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		class Maker {
			constructor(v) { this.v = v; }
			static create(v) { return new Maker(v); }
		}
		var obj = Maker.create(42);
		var val = obj.v;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("val"); v.Number() != 42 {
		t.Errorf("expected val=42, got %v", v.Number())
	}
}

// TestMCP_JSONRPCRoundtrip tests JSON parse/stringify for MCP protocol messages.
func TestMCP_JSONRPCRoundtrip(t *testing.T) {
	vm := New()
	_, err := vm.Run(`
		var msg = '{"jsonrpc":"2.0","id":1,"method":"tools/list"}';
		var obj = JSON.parse(msg);
		var method = obj.method;
		var back = JSON.stringify(obj);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v := vm.Get("method"); v.String() != "tools/list" {
		t.Errorf("expected method='tools/list', got '%s'", v.String())
	}
}

// TestMCP_MCPServerExample runs the actual MCP server example with test messages.
func TestMCP_MCPServerExample(t *testing.T) {
	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()

	// Run the MCP server code inline (simplified version)
	_, err := vm.Run(`
		var SERVER_NAME = "test-server";
		var tools = {};

		function defineTool(name, description, inputSchema, handler) {
			tools[name] = { name: name, description: description, inputSchema: inputSchema, handler: handler };
		}

		defineTool("add", "Add two numbers", {
			type: "object",
			properties: { a: { type: "number" }, b: { type: "number" } },
			required: ["a", "b"]
		}, function(params) { return String(params.a + params.b); });

		function handleMessage(msg) {
			var method = msg.method;
			if (method === "initialize") {
				return { jsonrpc: "2.0", id: msg.id, result: { serverInfo: { name: SERVER_NAME } } };
			}
			if (method === "tools/list") {
				var toolList = [];
				var names = Object.keys(tools);
				for (var i = 0; i < names.length; i++) {
					var t = tools[names[i]];
					toolList.push({ name: t.name, description: t.description });
				}
				return { jsonrpc: "2.0", id: msg.id, result: { tools: toolList } };
			}
			if (method === "tools/call") {
				var tool = tools[msg.params.name];
				var result = tool.handler(msg.params.arguments);
				return { jsonrpc: "2.0", id: msg.id, result: { content: [{ type: "text", text: result }] } };
			}
			return null;
		}

		// Test initialize
		var initResp = handleMessage({ id: 1, method: "initialize" });
		var initJSON = JSON.stringify(initResp);

		// Test tools/list
		var listResp = handleMessage({ id: 2, method: "tools/list" });
		var listJSON = JSON.stringify(listResp);

		// Test tools/call
		var callResp = handleMessage({ id: 3, method: "tools/call", params: { name: "add", arguments: { a: 3, b: 4 } } });
		var callJSON = JSON.stringify(callResp);
	`)
	if err != nil {
		t.Fatal(err)
	}

	initJSON := vm.Get("initJSON").String()
	if initJSON == "" || initJSON == "undefined" {
		t.Error("initialize response is empty")
	}

	listJSON := vm.Get("listJSON").String()
	if listJSON == "" || listJSON == "undefined" {
		t.Error("tools/list response is empty")
	}

	callJSON := vm.Get("callJSON").String()
	if callJSON == "" || callJSON == "undefined" {
		t.Error("tools/call response is empty")
	}

	// Verify the add tool returned 7
	callResp := vm.Get("callResp")
	if callResp.typ == TypeObject {
		result := callResp.getProp("result")
		content := result.getProp("content")
		if content.typ == TypeArray && len(content.array) > 0 {
			text := content.array[0].getProp("text").String()
			if text != "7" {
				t.Errorf("expected add result='7', got '%s'", text)
			}
		} else {
			t.Error("unexpected content structure")
		}
	}
}

// TestMCP_RealSDKWithZod4 loads the actual @modelcontextprotocol/sdk and zod v4
// and verifies that McpServer can be constructed and tools registered.
func TestMCP_RealSDKWithZod4(t *testing.T) {
	dir := mcpTestDir()

	vm := New()
	el := NewEventLoop(vm)
	el.RegisterGlobals()
	RegisterBuffer(vm)
	ms := NewModuleSystem(vm, dir)
	ms.RegisterGlobals("")

	_, err := vm.Run(`
		var z = require("zod");
		var mcpMod = require("@modelcontextprotocol/sdk/server/mcp.js");
		var McpServer = mcpMod.McpServer;

		var server = new McpServer({ name: "test-server", version: "1.0.0" });

		server.tool(
			"add",
			"Add two numbers",
			{ a: z.number(), b: z.number() },
			function(params) {
				return { content: [{ type: "text", text: String(params.a + params.b) }] };
			}
		);

		var hasTool = typeof server._registeredTools === "object" &&
			typeof server._registeredTools["add"] === "object";
		var toolDescription = server._registeredTools["add"].description;
		var handlerType = typeof server._registeredTools["add"].handler;
	`)
	if err != nil {
		t.Fatalf("real MCP SDK + zod4 failed: %v", err)
	}
	if v := vm.Get("hasTool"); !v.truthy() {
		t.Error("expected server._registeredTools.add to exist")
	}
	if v := vm.Get("toolDescription"); v.String() != "Add two numbers" {
		t.Errorf("expected tool description='Add two numbers', got '%s'", v.String())
	}
	if v := vm.Get("handlerType"); v.String() != "function" {
		t.Errorf("expected handler typeof='function', got '%s'", v.String())
	}
}
