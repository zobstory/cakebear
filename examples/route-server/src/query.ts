// Pulling the path and a query parameter out of req.url, with the string
// methods of the M4 strings step.

// pathOf is url up to its query.
export function pathOf(url: string): string {
  const q: number = url.indexOf("?");
  if (q < 0) {
    return url;
  }
  return url.slice(0, q);
}

// param is the decoded value of name in url's query, or "" when it has none.
export function param(url: string, name: string): string {
  const q: number = url.indexOf("?");
  if (q < 0) {
    return "";
  }
  for (const pair of url.slice(q + 1).split("&")) {
    const eq: number = pair.indexOf("=");
    if (eq > 0 && pair.slice(0, eq) === name) {
      return decodeURIComponent(pair.slice(eq + 1).replaceAll("+", " "));
    }
  }
  return "";
}
