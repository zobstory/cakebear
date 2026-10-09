// cakebear's node:http declarations.
//
// Served from a virtual path and added as a root of every program, the same
// way cakebear.d.ts is, so `import { createServer } from "node:http"` resolves
// with no npm install and no @types/node. TypeScript 7 no longer includes
// @types/* automatically, and cakec reads no tsconfig, so @types/node would
// only work behind a `/// <reference types="node" />` line anyway.
//
// It declares exactly what cakebear's runtime implements (M1), and nothing
// else. Calling an unimplemented API is then a type error at the call site,
// in TypeScript's own words ("Property 'flushHeaders' does not exist on type
// 'ServerResponse'"), instead of a lowering refusal.
//
// Shapes follow @types/node so programs stay portable, with one deliberate
// narrowing: @types/node types `url` and `method` as `string | undefined`,
// because IncomingMessage is also used for client responses. A server request
// always has both, and cakebear has no nullable representation yet.
//
// Lowering recognises these APIs by their declaring file, as it does the
// conversion functions in cakebear.d.ts: a user's own `end` method is not
// cakebear's.

declare module "node:http" {
  export interface IncomingMessage {
    readonly method: string;
    readonly url: string;
  }

  export interface ServerResponse {
    setHeader(name: string, value: string): this;
    writeHead(statusCode: number): this;
    end(chunk?: string): this;
  }

  export interface Server {
    listen(port: number, listeningListener?: () => void): this;
  }

  export function createServer(
    requestListener: (req: IncomingMessage, res: ServerResponse) => void,
  ): Server;
}

declare module "http" {
  export * from "node:http";
}
