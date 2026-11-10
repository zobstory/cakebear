// A server in a workspace: the app imports its own modules through the `@/`
// alias, and a forecast package linked through node_modules, whose barrel
// re-exports a frozen const and a default export. Built from this
// directory's tsconfig.json into one native binary, it serves what Node
// serves from these same sources:
//
//   cakec build -p examples/workspace && examples/workspace/bin/server
//   cd examples/workspace && node --import ../../cmd/cakec/testdata/paths-hook.mjs app/main.ts
//
//   curl localhost:3000/        # the forecast, as a page of HTML
//   curl localhost:3000/tue     # one day, as text
//   curl localhost:3000/thu     # 404
import { createServer } from "node:http";
import { days, describe, type Day } from "@example/forecast";
import page from "@/page.ts";

const port: number = 3000;

createServer((req, res) => {
  const url: string = req.url;
  if (url === "/") {
    res.setHeader("Content-Type", "text/html; charset=utf-8");
    res.end(page("Three days"));
    return;
  }
  const day: Day | undefined = days.find((d) => `/${d.name.toLowerCase()}` === url);
  if (day !== undefined) {
    res.setHeader("Content-Type", "text/plain; charset=utf-8");
    res.end(describe(day) + "\n");
    return;
  }
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.writeHead(404);
  res.end("not found\n");
}).listen(port, () => {
  console.log(`listening on http://localhost:${port}`);
});
