// Pulling the path and a query parameter out of req.url, with the string
// methods of the M4 strings step, and throwing what is wrong with them (M6).

// pathOf is url up to its query.
export function pathOf(url: string): string {
  const q: number = url.indexOf("?");
  if (q < 0) {
    return url;
  }
  return url.slice(0, q);
}

// param is the decoded value of name in url's query, or undefined when it has
// none. A malformed escape, `?q=%E0`, is decodeURIComponent's URIError, which
// route answers with a 400 too.
export function param(url: string, name: string): string | undefined {
  const q: number = url.indexOf("?");
  if (q < 0) {
    return undefined;
  }
  for (const pair of url.slice(q + 1).split("&")) {
    const eq: number = pair.indexOf("=");
    if (eq > 0 && pair.slice(0, eq) === name) {
      return decodeURIComponent(pair.slice(eq + 1).replaceAll("+", " "));
    }
  }
}

// count is name's value in url's query as a whole number, or fallback when it
// is missing or empty. Any other value is a bad query, a RangeError, which
// route answers with a 400.
export function count(url: string, name: string, fallback: number = 0): number {
  const v: string | undefined = param(url, name);
  if (!v) {
    return fallback;
  }
  const n: number = Number(v);
  if (!Number.isInteger(n) || n < 0) {
    throw new RangeError(`${name} must be a whole number, not "${v}"`);
  }
  return n;
}
