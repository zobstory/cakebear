// What a case marked `// shim` runs under, with node --import: cakebear's
// spawn global as Node can run it, on the same thread and without the
// parallelism (CLAUDE.md, the accepted differences).
globalThis.spawn = (fn) => Promise.resolve().then(fn);
