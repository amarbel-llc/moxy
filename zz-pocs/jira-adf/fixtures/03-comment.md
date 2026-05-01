Update from investigation:

- Reproduced on `staging-2` after running `make seed` with the canary fixture.
- Root cause is in `app/handler.py:42` — the early-return path skips logging.
- Filed follow-up ticket and linked it via `Closes #N`.

Next steps:

1. Land the patch in `release/24.04`.
2. Backport to `release/23.10` if the regression test passes.
