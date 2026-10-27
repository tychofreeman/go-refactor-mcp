/** A plant in a bed. */
export interface Plant {
  name: string;
  height: number;
}

export type Height = number;

/** Bed holds plants and waters them. */
export class Bed {
  private plants: Plant[] = [];

  constructor(public readonly soil: string) {}

  /** plant adds p to the bed. */
  plant(p: Plant): void {
    this.plants.push(p);
    this.plants.sort((a, b) => a.height - b.height);
  }

  get tallest(): Plant | undefined {
    const n = this.plants.length;
    return n > 0 ? this.plants[n - 1] : undefined;
  }
}

export const water = (liters: number): string => {
  const ml = liters * 1000;
  return `${ml} ml`;
};

export function prune(p: Plant, by: number): Plant {
  const height = Math.max(0, p.height - by);
  return { ...p, height };
}
