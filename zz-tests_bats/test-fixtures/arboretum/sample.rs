// Sample Rust file.

pub mod greetings {
    pub const DEFAULT_NAME: &str = "world";

    pub fn shout(s: &str) -> String {
        s.to_uppercase()
    }
}

pub struct Counter {
    pub n: u32,
    seed: u32,
}

impl Counter {
    pub fn new(seed: u32) -> Self {
        Counter { n: 0, seed }
    }

    pub fn inc(&mut self) {
        self.n += 1;
    }
}

pub trait Greeter {
    fn greet(&self, name: &str) -> String;
}

pub enum Color {
    Red,
    Green(u8),
    Blue { hex: String },
}

pub type Score = u32;

static GLOBAL: u32 = 42;
