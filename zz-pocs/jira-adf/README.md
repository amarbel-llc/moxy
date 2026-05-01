# jira-adf POC

Throwaway scratch space exploring Markdown ↔ ADF (Atlassian Document Format)
conversion for the sisyphus moxin's eventual Jira REST API v2 → v3 migration.

Tracking issues:

- [`#203`](https://github.com/amarbel-llc/moxy/issues/203) — overall migration
- [`#204`](https://github.com/amarbel-llc/moxy/issues/204) — converter library
  evaluation

## What this proves

`atlassian-python-api 4.0.7` defaults to `api_version="2"` but exposes a
constructor flag (`Jira(..., api_version="3")`) that flips every endpoint to
v3. On v3, body fields (`description`, comment `body`) require ADF JSON
instead of wiki markup. The question this POC answers is: **is there a
pure-Python library that round-trips Markdown ↔ ADF cleanly enough that we
could plug it into the three body-sensitive sisyphus tools?**

## Candidates exercised

- [`adflux`](https://github.com/mikejhill/adflux) — pure-Python, panflute IR
  + declarative `mapping.yaml` + envelope nodes for ADF-only constructs.
  Documents lossless round-trip and has a `jira-strict` option.
- [`marklas`](https://github.com/byExist/marklas) — pure-Python,
  markdown-it-py + custom AST. ADF-only constructs preserved as HTML
  attributes.

## Run

```sh
just round-trip            # both libraries, all sample fixtures
just round-trip-adflux     # just adflux
just round-trip-marklas    # just marklas
just diff sample           # show MD-in vs MD-out side by side for one sample
```

## Status

POC. Throwaway. Not part of the build.
