// rule: limit0
// paths: /boom /string /after /header
import { createServer } from "node:http";
const server = createServer((req, res) => {
  if (req.url === "/ok") {
    res.end("ok");
    return;
  }
  if (req.url === "/string") throw "a string";
  if (req.url === "/after") {
    res.end("partial");
    throw new RangeError("after the end");
  }
  if (req.url === "/header") res.setHeader("bad name", "v");
  throw new Error("before the end");
});
server.listen(Number(process.env.PORT), () => console.log("listening"));
