// Sample JavaScript file.

function shout(s) {
  return s.toUpperCase();
}

class Counter {
  constructor(start) {
    this.n = start;
  }

  inc() {
    this.n++;
  }

  value() {
    return this.n;
  }
}

const DEFAULT = 0;
let total = 0;

const greet = (name) => `hi, ${name}`;
