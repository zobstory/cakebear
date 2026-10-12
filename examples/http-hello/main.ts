// A node:http server. It runs unmodified under Node, and under cakebear it
// compiles to a native binary in which every request runs on its own
// goroutine, in parallel:
//
//   cakec build examples/http-hello/main.ts -o hello && ./hello
//   curl localhost:3000/          # hello from cakebear
//   curl localhost:3000/health    # ok
import { createServer } from "node:http";

const server = createServer((req, res) => {
  res.setHeader("Content-Type", "text/plain");
  if (req.url === "/health") {
    res.end("ok\n");
  } else {
    res.end("hello from cakebear\n");
  }
});

server.listen(3000, () => {
  console.log("listening on http://localhost:3000");
});
