// End-to-end smoke: load real SDK + zod v4, register tools, invoke one directly
const { McpServer } = require("@modelcontextprotocol/sdk/server/mcp.js");
const { z } = require("zod");

const server = new McpServer({
    name: "espresso-real-mcp",
    version: "1.0.0",
});

server.tool(
    "add",
    "Add two numbers",
    { a: z.number(), b: z.number() },
    async ({ a, b }) => ({
        content: [{ type: "text", text: String(a + b) }],
    })
);

server.tool(
    "hello",
    "Greet someone",
    { name: z.string() },
    async ({ name }) => ({
        content: [{ type: "text", text: "Hello, " + name + "!" }],
    })
);

const tools = server._registeredTools;
process.stdout.write("registered tools: " + Object.keys(tools).join(",") + "\n");
process.stdout.write("add.description: " + tools.add.description + "\n");
process.stdout.write("hello.description: " + tools.hello.description + "\n");
process.stdout.write("add.handler typeof: " + typeof tools.add.handler + "\n");
process.stdout.write("hello.handler typeof: " + typeof tools.hello.handler + "\n");

// Invoke the handler directly
const r = tools.add.handler({ a: 3, b: 4 });
process.stdout.write("add(3,4) result type: " + typeof r + "\n");
if (r && typeof r.then === "function") {
    r.then(v => process.stdout.write("add(3,4) resolved: " + JSON.stringify(v) + "\n"));
} else {
    process.stdout.write("add(3,4) sync: " + JSON.stringify(r) + "\n");
}
