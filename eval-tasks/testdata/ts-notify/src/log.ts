/** The lines logged, in order. */
export const logged: string[] = [];

/** Logs a line. */
export function log(line: string): void {
  logged.push(line);
}
