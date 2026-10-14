import type { IncomingMessage, ServerResponse } from "node:http";
import { greeting } from "./greeting.ts";

// handle runs for every request, and under cakebear requests run in parallel,
// so it may share nothing that can change. The race rule checks that through
// the modules it imports too.
export function handle(req: IncomingMessage, res: ServerResponse): void {
  res.setHeader("Content-Type", "text/plain");
  if (req.url === "/health") {
    res.end("ok\n");
  } else {
    res.end(greeting("cakebear") + "\n");
  }
}
