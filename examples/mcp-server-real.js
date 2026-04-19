// Real MCP server using @modelcontextprotocol/sdk + zod v4
const { McpServer } = require("@modelcontextprotocol/sdk/server/mcp.js");
const { StdioServerTransport } = require("@modelcontextprotocol/sdk/server/stdio.js");
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

const transport = new StdioServerTransport();
server.connect(transport).then(() => {
    process.stderr.write("MCP server running on stdio\n");
});
