/** Throws unless got and want are equal, compared as JSON. */
export function equal(got: unknown, want: unknown, what: string): void {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) {
    throw new Error(`${what} = ${g}, want ${w}`);
  }
}
