# Vendored third-party code

This directory contains vendored third-party Python source. Each library is
copied verbatim from an upstream tagged release, with its license retained
alongside.

## marklas

- **Upstream:** https://github.com/byExist/marklas
- **Version:** v0.7.0
- **Tarball:** https://codeload.github.com/byExist/marklas/tar.gz/refs/tags/v0.7.0
- **Tarball sha256:** `3bb8e1254d6c7f23ae67fbb53aa5400cdd992b6d840f4961f3421be22aa98cb1`
- **License:** MIT — see `MARKLAS-LICENSE`
- **Copyright:** (c) 2025 byExist

### Why vendored

`marklas` is the bidirectional Markdown ↔ Atlassian Document Format (ADF)
converter used by sisyphus to power the Jira REST API v3 migration
(see [issue #203](https://github.com/amarbel-llc/moxy/issues/203) and
[#204](https://github.com/amarbel-llc/moxy/issues/204)). It is not packaged in
nixpkgs at the moment.

Vendoring keeps the version pinned to the tree, avoids a `buildPythonPackage`
override in `flake.nix`, and makes the conversion behavior exactly
reproducible across builds. The single transitive Python dependency
(`mistune`) is provided by `sisyphus-python` in `flake.nix`.

### Updating

Bump the version in this file, then re-run the fetch (verifying the new
tarball sha256):

```sh
tmp=$(mktemp -d)
curl -sSL "https://codeload.github.com/byExist/marklas/tar.gz/refs/tags/<NEW_TAG>" \
  -o "$tmp/marklas.tar.gz"
sha256sum "$tmp/marklas.tar.gz"
tar -xzf "$tmp/marklas.tar.gz" -C "$tmp"
rm -rf moxins/sisyphus/lib/_vendor/marklas
cp -r "$tmp/marklas-<NEW_TAG_NO_V>/src/marklas" moxins/sisyphus/lib/_vendor/marklas
cp     "$tmp/marklas-<NEW_TAG_NO_V>/LICENSE"    moxins/sisyphus/lib/_vendor/MARKLAS-LICENSE
```

Then update the version, tarball URL, and sha256 above.
