# Phase 5 operator corpus manifest

The savings qualification spike accepts an operator-supplied JSON corpus
manifest with schema `restoreweave.corpus-manifest.v1`:

```json
{
  "schema": "restoreweave.corpus-manifest.v1",
  "entries": [{"path": "relative/name", "bytes": 12, "sha256": "..."}],
  "digest": "..."
}
```

`digest` is the SHA-256 of canonical JSON containing only `schema` and the
sorted `entries` (the digest field is omitted). Paths use one canonical,
slash-separated relative spelling (no `.`/`..`, empty components, or
backslashes), and SHA-256 values are lowercase hexadecimal. The runner reads
the manifest, rejects unknown fields, malformed entries, duplicate or unsafe
paths, and digest mismatches, then rescans the supplied corpus. Every path,
logical byte length, and SHA-256 must match exactly. Missing or extra files,
symlinks, special files, and changed bytes fail closed before any repository
operation.

Use `--corpus-manifest FILE` with `scripts/qualification-spike.sh` together
with `--existing-corpus DIR`. The resulting report remains candidate evidence;
an operator manifest does not make a repository profile representative or
release-qualified.
