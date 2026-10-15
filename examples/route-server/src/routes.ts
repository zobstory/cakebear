import { items, service } from "./data.ts";
import type { Item } from "./data.ts";
import { param, pathOf } from "./query.ts";

// Reply is what a route answers with.
export interface Reply {
  status: number;
  type: string;
  body: string;
}

function json(status: number, body: string): Reply {
  return { status: status, type: "application/json", body: body + "\n" };
}

function notFound(what: string): Reply {
  return json(404, JSON.stringify({ error: `no ${what}` }));
}

// route answers one request. Under cakebear requests run in parallel, so it
// may share nothing that can change: items and service are frozen, and what
// it builds is its own. The race rule checks that, through every module.
export function route(method: string, url: string): Reply {
  if (method !== "GET") {
    return json(405, JSON.stringify({ error: `${method} is not allowed` }));
  }
  const path: string = pathOf(url);
  if (path === "/") {
    return json(200, JSON.stringify({ service: service.name, version: service.version, items: items.length }));
  }
  if (path === "/health") {
    return { status: 200, type: "text/plain", body: "ok\n" };
  }
  if (path === "/items") {
    const tag: string = param(url, "tag");
    const q: string = param(url, "q").toLowerCase();
    const found: Item[] = items.filter((it: Item): boolean => (tag === "" || it.tags.includes(tag)) && it.name.toLowerCase().includes(q));
    return json(200, JSON.stringify(found));
  }
  if (path.startsWith("/items/")) {
    const id: number = Number(path.slice("/items/".length));
    const at: number = items.findIndex((it: Item): boolean => it.id === id);
    if (at < 0) {
      return notFound(`item ${path.slice(7)}`);
    }
    return json(200, JSON.stringify(items[at], null, 2));
  }
  if (path === "/menu") {
    const lines: string[] = items.map((it: Item): string => `${it.name.padEnd(14)} $${it.price.toFixed(2)}`);
    return { status: 200, type: "text/plain; charset=utf-8", body: lines.join("\n") + "\n" };
  }
  return notFound(`route ${path}`);
}
