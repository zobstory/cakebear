// rule: rejection-value
// paths: /string /number
// The same for a reason that isn't an Error, which Node reports as R13's
// UnhandledPromiseRejection and cakebear points at the throw.
import { createServer } from "node:http";
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
const server = createServer(async (req, res) => {
  if (req.url === "/ok") {
    res.end("ok");
    return;
  }
  if (req.url === "/string") throw "a string";
  await tick();
  throw 42;
});
server.listen(Number(process.env.PORT), () => console.log("listening"));
