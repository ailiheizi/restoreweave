# Recovery and boundaries

RestoreWeave keeps the physical core small:

- one configured SQLite catalog for durable metadata and rebuildable
  projections;
- a content repository for exact bytes and admitted representations;
- disposable search generations and optional model workers.

The repository plus authenticated portable recovery records are the recovery
authority. Removing SQLite, a vector index, or an optional model must not erase
the ability to verify and restore an exact file.

The current profile does not run destructive garbage collection. Reachability
information remains a non-destructive plan, and membership in a group or an
index never authorizes deleting bytes.

The following are intentionally later work rather than hidden defaults:

- multiple concurrently writable repository backends;
- release selection of Restic, Kopia, Plakar, or another engine;
- CLIP/SigLIP and music-feature packs;
- neural/RWKV compression;
- automatic URL reacquisition, source deletion, or a filesystem mount.

This boundary keeps the daily product understandable: save exact content,
describe it, find it, and restore it even when optional analysis is offline.
