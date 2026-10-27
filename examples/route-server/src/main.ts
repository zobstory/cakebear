// A JSON API in four modules: a frozen menu with optional properties
// (data.ts), query parsing with the string methods (query.ts), and the routes
// (routes.ts), which use ?., ??, truthiness and conditional spread, and throw
// what they can't answer, caught at the top of route. Built from this
// directory's tsconfig.json into one native binary, it serves what Node serves
// from these same sources, unmodified:
//
//   cakec build -p examples/route-server && examples/route-server/bin/server
//   node examples/route-server/src/main.ts
//
//   curl localhost:3000/                    # the service, as JSON
//   curl localhost:3000/items               # every item
//   curl 'localhost:3000/items?tag=hot'     # filtered by tag
//   curl 'localhost:3000/items?q=CAKE'      # searched by name
//   curl 'localhost:3000/items?limit=2'     # the first two
//   curl 'localhost:3000/items?limit=abc'   # 400, a RangeError caught
//   curl 'localhost:3000/items?q=%E0'       # 400, decodeURIComponent's URIError
//   curl localhost:3000/items/2             # one item, indented
//   curl localhost:3000/items/9             # 404, an Error caught
//   curl 'localhost:3000/first?tag=cold'    # the first item's name with a tag
//   curl localhost:3000/menu                # a plain-text menu
//   curl -X POST localhost:3000/            # 405
import { createServer } from "node:http";
import { route } from "./routes.ts";
import type { Reply } from "./routes.ts";

const port: number = 3000;

createServer((req, res) => {
  const reply: Reply = route(req.method, req.url);
  res.setHeader("Content-Type", reply.type);
  res.writeHead(reply.status);
  res.end(reply.body);
}).listen(port, () => {
  console.log(`listening on http://localhost:${port}`);
});
