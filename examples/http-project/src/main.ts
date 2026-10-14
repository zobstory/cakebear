// A node:http server in three modules, built from this directory's
// tsconfig.json into one native binary. Node runs the same sources unmodified:
//
//   cakec build -p examples/http-project && examples/http-project/bin/server
//   node examples/http-project/src/main.ts
//   curl localhost:3000/          # hello, cakebear
//   curl localhost:3000/health    # ok
import { createServer } from "node:http";
import { handle } from "./routes.ts";

const server = createServer(handle);

server.listen(3000, () => {
  console.log("listening on http://localhost:3000");
});
