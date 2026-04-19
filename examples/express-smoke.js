// Smoke test: load express, create app, register a route, invoke handler
const express = require("express");
process.stdout.write("express typeof: " + typeof express + "\n");

const app = express();
process.stdout.write("app typeof: " + typeof app + "\n");
process.stdout.write("app.get typeof: " + typeof app.get + "\n");
process.stdout.write("app.use typeof: " + typeof app.use + "\n");

app.get("/hello", (req, res) => {
    res.send("Hello world!");
});

app.get("/users/:id", (req, res) => {
    res.json({ id: req.params.id });
});

process.stdout.write("routes registered\n");
// Inspect the router
if (app._router) {
    process.stdout.write("has _router: true, stack len: " + (app._router.stack ? app._router.stack.length : "?") + "\n");
}
