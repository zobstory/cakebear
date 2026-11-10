import type { Day } from "./days.ts";

export default function describe(d: Day): string {
  return `${d.name}: ${d.sky}, ${d.high}° / ${d.low}°`;
}
