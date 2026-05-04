// Sample TypeScript file.

export type Status = "ok" | "fail";

export interface Greeter {
  greet(name: string): string;
  count: number;
}

export class HelloGreeter implements Greeter {
  count: number = 0;

  greet(name: string): string {
    this.count++;
    return `Hello, ${name}!`;
  }
}

export function createGreeter(): Greeter {
  return new HelloGreeter();
}

export enum Color {
  Red,
  Green,
  Blue,
}

const DEFAULT_NAME = "world";

let mutableState: number = 0;
