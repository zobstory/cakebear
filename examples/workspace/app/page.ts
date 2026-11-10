import { days, describe } from "@example/forecast";
import { escape } from "@/html.ts";

// The forecast as a page of HTML, every value escaped.
export default function page(title: string): string {
  const rows: string[] = days.map((d) => `  <li><a href="/${d.name.toLowerCase()}">${escape(describe(d))}</a></li>`);
  return `<!doctype html>\n<title>${escape(title)}</title>\n<ul>\n${rows.join("\n")}\n</ul>\n`;
}
