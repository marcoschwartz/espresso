// MCP Server running on Espresso JS engine
// Uses stdio transport (JSON-RPC 2.0 over stdin/stdout)

const SERVER_NAME = "espresso-tools";
const SERVER_VERSION = "1.0.0";
const PROTOCOL_VERSION = "2024-11-05";

// Tool registry
const tools = {};

function defineTool(name, description, inputSchema, handler) {
    tools[name] = {
        name: name,
        description: description,
        inputSchema: inputSchema,
        handler: handler
    };
}

// ── Define tools ──

defineTool("hello", "Say hello to someone", {
    type: "object",
    properties: {
        name: { type: "string", description: "Name to greet" }
    },
    required: ["name"]
}, (params) => {
    return "Hello, " + params.name + "!";
});

defineTool("add", "Add two numbers", {
    type: "object",
    properties: {
        a: { type: "number", description: "First number" },
        b: { type: "number", description: "Second number" }
    },
    required: ["a", "b"]
}, (params) => {
    return String(params.a + params.b);
});

defineTool("echo", "Echo back any text", {
    type: "object",
    properties: {
        text: { type: "string", description: "Text to echo" }
    },
    required: ["text"]
}, (params) => {
    return params.text;
});

// ── JSON-RPC handler ──

function handleMessage(msg) {
    const method = msg.method;

    if (method === "initialize") {
        return {
            jsonrpc: "2.0",
            id: msg.id,
            result: {
                protocolVersion: PROTOCOL_VERSION,
                capabilities: { tools: {} },
                serverInfo: {
                    name: SERVER_NAME,
                    version: SERVER_VERSION
                }
            }
        };
    }

    if (method === "notifications/initialized") {
        return null;
    }

    if (method === "tools/list") {
        const toolList = [];
        const names = Object.keys(tools);
        for (var i = 0; i < names.length; i++) {
            const t = tools[names[i]];
            toolList.push({
                name: t.name,
                description: t.description,
                inputSchema: t.inputSchema
            });
        }
        return {
            jsonrpc: "2.0",
            id: msg.id,
            result: { tools: toolList }
        };
    }

    if (method === "tools/call") {
        const toolName = msg.params.name;
        const toolArgs = msg.params.arguments;
        const tool = tools[toolName];
        if (!tool) {
            return {
                jsonrpc: "2.0",
                id: msg.id,
                error: {
                    code: -32602,
                    message: "Unknown tool: " + toolName
                }
            };
        }
        const result = tool.handler(toolArgs);
        return {
            jsonrpc: "2.0",
            id: msg.id,
            result: {
                content: [{ type: "text", text: result }]
            }
        };
    }

    // Unknown method
    return {
        jsonrpc: "2.0",
        id: msg.id,
        error: {
            code: -32601,
            message: "Method not found: " + method
        }
    };
}

// ── Stdio transport ──

process.stderr.write("MCP server '" + SERVER_NAME + "' started on stdio\n");

process.stdin.on("line", (line) => {
    if (line.trim() === "") {
        return;
    }

    const msg = JSON.parse(line);
    const response = handleMessage(msg);

    if (response !== null) {
        process.stdout.write(JSON.stringify(response) + "\n");
    }
});

process.stdin.on("end", () => {
    process.stderr.write("MCP server shutting down\n");
});
