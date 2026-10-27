export function prune(): void {}

const key = "label";

export class Shed {
  [key](): string {
    const name = "shed";
    return name;
  }

  #lock(): void {
    console.log("locked");
    console.log("again");
  }
}
