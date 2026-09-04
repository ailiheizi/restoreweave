# Storage and capacity

## One storage scheme, explained simply

A storage scheme is one named choice that bundles:

```text
name + location + storage engine + deduplication + compression
```

Choosing the scheme therefore also chooses how bytes are placed. The core
identity does not change: it is always the complete byte stream's SHA-256 plus
its logical length.

The same logical file may eventually have placements in more than one scheme,
but it remains one file in the catalog. Schemes do not silently copy data to
one another or replace an unavailable destination.

## What exists now

The current daemon has one active repository path. The in-tree Directory CAS
profile is the development profile. The local zstd profile is a measurable
candidate, not a release-qualified default. Restic, Kopia, and Plakar remain
qualification candidates rather than arbitrary engine choices in the UI.

Future named schemes can make “local disk”, “NAS path”, or “backup disk” easy
to select without adding another metadata database. That work needs a reviewed
configuration and placement contract after repository qualification.

## Space shown to the operator

When a backend can measure it, a scheme should report:

- total filesystem capacity;
- free capacity;
- RestoreWeave's physical bytes, when measurable;
- measurement time and health.

If a backend cannot provide a reliable value, the UI says **unknown**. An
unknown capacity value does not by itself prevent an exact save. Logical file
size, duplicate savings, compression savings, and filesystem free space are
different numbers and are not added together.

Similarity, embeddings, and filenames never perform exact deduplication. A
future chunked scheme must still verify every reconstructed file against its
SHA-256 identity before it can be admitted.
