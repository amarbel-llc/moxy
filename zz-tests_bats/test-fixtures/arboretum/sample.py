"""Sample Python module."""

DEFAULT_NAME = "world"


def shout(s: str) -> str:
    return s.upper()


class Counter:
    def __init__(self, start: int = 0) -> None:
        self.n = start

    def inc(self) -> None:
        self.n += 1

    @property
    def value(self) -> int:
        return self.n


class Greeter:
    def greet(self, name: str) -> str:
        return f"hello, {name}"
