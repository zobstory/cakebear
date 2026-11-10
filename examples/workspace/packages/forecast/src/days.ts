// A frozen const: readonly all the way down and built from literals, so every
// request may read it at once.
export interface Day {
  readonly name: string;
  readonly high: number;
  readonly low: number;
  readonly sky: string;
}

export const days: readonly Day[] = [
  { name: "Mon", high: 21, low: 12, sky: "sun" },
  { name: "Tue", high: 18, low: 11, sky: "rain & wind" },
  { name: "Wed", high: 23, low: 14, sky: "cloud" },
];
