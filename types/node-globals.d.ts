// cakebear's Node globals: process.env, read-only (S1 plan D8).
//
// Served from a virtual path and added as a root of every program, as
// node-http.d.ts is, so a server reads its configuration with no npm install
// and no @types/node. A native build of a program that has @types/node
// leaves this file out with node-http.d.ts, and lowers @types/node's own
// declaration of process.env (lower/env.go).
//
// It declares what the runtime implements and nothing else: a write to
// process.env, or any other member of process, is a type error in
// TypeScript's own words. Writes stay out because the environment would be
// state every request shares, and the table that never changes is what lets
// a handler read it.

declare var process: {
  readonly env: { readonly [name: string]: string | undefined };
};
