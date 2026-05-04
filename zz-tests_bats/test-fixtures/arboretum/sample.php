<?php
// Sample PHP file.

namespace SampleNs;

const DEFAULT_NAME = "world";

function shout(string $s): string {
    return strtoupper($s);
}

interface Greeter {
    public function greet(string $name): string;
}

trait Countable {
    public int $count = 0;

    public function tick(): void {
        $this->count++;
    }
}

class HelloGreeter implements Greeter {
    use Countable;

    public string $salutation = "hi";

    public function greet(string $name): string {
        $this->tick();
        return "{$this->salutation}, {$name}";
    }
}
