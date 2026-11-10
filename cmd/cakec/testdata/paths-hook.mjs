// paths-hook.mjs: runs sources that import through tsconfig `paths` under
// Node, which never reads a tsconfig (M7 plan D10, R20, R24):
//
//   node --import <this file> src/main.ts
//
// from the directory of the tsconfig.json. A resolve hook, registered in
// thread with module.registerHooks, maps a specifier through the config's
// compilerOptions.paths as TypeScript does for one "*" per pattern, the
// longest prefix first, to the first target that exists. Anything else goes
// to Node unchanged. Measured on Node 24.18: Node's output with it is byte for
// byte the same, uncaught reports included (R24), where --experimental-loader
// adds a warning and a frame.
import fs from "node:fs";
import { registerHooks } from "node:module";
import path from "node:path";
import { pathToFileURL } from "node:url";

const config = path.resolve("tsconfig.json");
const base = path.dirname(config);
const rules = Object.entries(JSON.parse(fs.readFileSync(config, "utf8")).compilerOptions?.paths ?? {})
  .map(([pattern, targets]) => {
    const star = pattern.indexOf("*");
    return {
      prefix: star < 0 ? pattern : pattern.slice(0, star),
      suffix: star < 0 ? "" : pattern.slice(star + 1),
      wildcard: star >= 0,
      targets: targets.map((t) => path.resolve(base, t)),
    };
  })
  .sort((a, b) => b.prefix.length - a.prefix.length);

function map(specifier) {
  for (const r of rules) {
    const hit = r.wildcard
      ? specifier.startsWith(r.prefix) && specifier.endsWith(r.suffix) &&
        specifier.length >= r.prefix.length + r.suffix.length
      : specifier === r.prefix;
    if (!hit) continue;
    const middle = r.wildcard ? specifier.slice(r.prefix.length, specifier.length - r.suffix.length) : "";
    for (const t of r.targets) {
      const file = t.replace("*", middle);
      if (fs.existsSync(file)) return pathToFileURL(file).href;
    }
  }
  return null;
}

registerHooks({
  resolve(specifier, context, next) {
    return next(map(specifier) ?? specifier, context);
  },
});
