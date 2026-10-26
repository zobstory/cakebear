// rule: limit0
import { createServer } from "node:http";
const server = createServer((_req, res) => res.end("ok"));
server.listen(0, () => {
  console.log("listening");
  throw new Error("from the listen callback");
});
