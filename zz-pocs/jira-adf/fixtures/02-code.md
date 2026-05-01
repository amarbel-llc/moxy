# Bug repro

Calling `do_thing()` with a `None` argument crashes:

```python
from app import do_thing

do_thing(None)  # raises TypeError
```

The fix is to guard the argument:

```python
def do_thing(arg):
    if arg is None:
        raise ValueError("arg is required")
    ...
```

A diff showing the same change in shell:

```sh
$ git diff
- do_thing(None)
+ do_thing("ok")
```
