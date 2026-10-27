// rule: header-from-frames
// paths: /before-await /after-await /after-end /after-end-await /after-end-timer /after-timer /unhandled
// M6 plan D8, m6-node's c2-h3 to c2-h7: an async handler's rejection nothing
// handles ends Node's whole server, and only its own request in cakebear,
// before the response ends by dropping the connection, after it leaving the
// response as it stands.
import { createServer } from "node:http";
function tick(): Promise<void> {
  return new Promise((resolve) => resolve());
}
function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
async function fail(): Promise<void> {
  throw new TypeError("nothing awaits this");
}
const server = createServer(async (req, res) => {
  if (req.url === "/ok") {
    res.end("ok");
    return;
  }
  if (req.url === "/before-await") throw new Error("before any await");
  if (req.url === "/after-end") {
    res.end("partial");
    throw new Error("after the end");
  }
  if (req.url === "/unhandled") {
    fail();
    res.end("sent");
    return;
  }
  if (req.url === "/after-timer") {
    await delay(5);
    throw new RangeError("after a timer");
  }
  if (req.url === "/after-end-await") {
    res.end("partial");
    await tick();
    throw new Error("after the end and an await");
  }
  if (req.url === "/after-end-timer") {
    res.end("partial");
    await delay(5);
    throw new Error("after the end and a timer");
  }
  await tick();
  throw new Error("after an await");
});
server.listen(Number(process.env.PORT), () => console.log("listening"));
