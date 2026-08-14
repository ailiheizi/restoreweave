# Namespace and Content Access Technical Design

## 1. Objective

RestoreWeave presents stored data as an authenticated, file-shaped namespace even when the physical bytes are deduplicated, chunked, compressed, encrypted, packed with unrelated files, transformed into another representation, or distributed across several repositories.

This is the stable data-access promise:

> A caller can browse, read, mount, and restore the original directory form without knowing how the repository physically stores the bytes.

The design targets self-hosted Linux-based NAS and large heterogeneous collections. It is independent of one repository engine or presentation protocol. Platform-specific capture drivers translate source semantics into the common namespace model and declare every fidelity limitation.

The file-shaped interface is the narrow waist shared by:

- CLI browse, stat, range read, and restore.
- Local read-only MCP resources and tools.
- Optional WebUI and REST adapters.
- Bundled read-only Linux FUSE access.
- Later SMB, NFS, WebDAV, S3-compatible, media-server, alternate FUSE, and other gateways.
- External text, media, vector, and hybrid search systems.

Related contracts are defined in:

- [Core Kernel and Interface Requirements](../requirements/core-kernel-and-interface.md).
- [Driver and Processor Interfaces](../requirements/driver-and-processor-interfaces.md).
- [CLI and MCP Contract](../requirements/cli-and-mcp-contract.md).
- [Core Protocol and Reference Userland](core-protocol-and-reference-userland.md).

## 2. Scope and non-goals

This design covers:

- Immutable historical namespace generations.
- Original path components and filesystem metadata.
- Exact file-version and content identity.
- Multiple exact or derived representations of one subject.
- Mapping logical files to repository placements and decoder graphs.
- Bounded browse, read, export, and exact restore.
- Incremental update, observed deletion, retention, and garbage-collection boundaries.
- Portable publication and clean-machine namespace reconstruction.
- Rebuildable operational, full-text, vector, and media indexes.

This design does not require the core to provide:

- A writable collaborative filesystem.
- A POSIX kernel filesystem implementation.
- An embedded search engine or vector database.
- A general AI harness or model runtime.
- Direct access to repository-private chunks, packs, or database rows.
- Silent conversion of approximate media into exact files.

## 3. Architectural invariants

1. Capture-owned records define path truth. Repository enumeration validates coverage but cannot invent or rename paths.
2. Namespace identity is independent of physical placement.
3. One exact byte sequence has one algorithm-qualified content identity within its digest domain, regardless of how many paths or snapshots reference it.
4. A file version binds content, metadata, and filesystem semantics at one captured point in time.
5. A representation is not a path. A path resolves to a file version, and the core selects an authorized representation for a requested access contract.
6. The default read and restore path selects an authoritative exact representation.
7. Similarity is not identity. Embeddings, perceptual hashes, acoustic fingerprints, captions, and classifier scores cannot satisfy an exact-byte request.
8. Historical snapshots are immutable. Updates and observed deletions create successor namespaces.
9. Logical deletion, snapshot retirement, representation retirement, and physical garbage collection are separate events.
10. Search and operational databases are rebuildable projections. RRF records and portable publication evidence remain authority.
11. Exact browse and restore work without a model service, index, WebUI, or original SQLite database.
12. Every unsupported source semantic is declared rather than silently flattened.
13. A safe logical interface cannot repair an unsafe capture. Only entries produced through a qualified source-root binding, traversal, and publication gate may become authoritative namespace records; later component-based lookup does not make ancestor substitution, mount replacement, or unsafe special-file access during capture acceptable.

## 4. Reference architecture

~~~mermaid
flowchart LR
    Source["Captured source view"] --> Scan["Namespace observation"]
    Scan --> RRF["Authenticated namespace records"]
    RRF --> Tree["SnapshotTree"]

    Bytes["Captured file bytes"] --> Exact["Host exact hash and identity"]
    Exact --> Repository["RepositoryDriver exact placement"]
    Repository --> ExactVerify["Host readback verification"]
    ExactVerify --> Reps["Admitted representation records"]

    Bytes -. "optional" .-> Process["Processor capabilities"]
    Process --> Stage["Host-controlled staging"]
    Stage --> Admit["Host validation and admission"]
    Admit --> Reps

    Tree --> Access["FileAccess"]
    Access --> Select["Core representation selection"]
    Select --> Reps
    Reps --> Places["Placement receipts"]
    Places --> Storage["Packed, deduplicated, compressed, encrypted storage"]
    Storage --> Decode["Verified decoder or reconstruction path"]
    Decode --> Access

    RRF --> Index["Rebuildable indexes"]
    AdapterRequest["Presentation adapter query"] --> Broker["Host query broker"]
    Broker --> ResolveGeneration["Resolve exactly one IndexGenerationRef"]
    ResolveGeneration --> Query["QueryProvider"]
    Index --> Query
    Query --> Reauthorize["Broker authoritative resolution and reauthorization"]
    Tree --> Reauthorize
    Reauthorize --> AdapterResponse["Presentation adapter results"]
~~~

The namespace plane and representation plane meet through stable file-version and subject identities. Neither a repository nor an index owns that join.

## 5. Common file-shaped namespace

### 5.1 Portable entry classes

The portable namespace supports these entry classes:

- Directory.
- Regular file.
- Symbolic link.
- Hard-link member.
- Sparse regular file.
- Named stream or fork when the capture profile supports it.
- Optional special node recorded as metadata when the source and restore profile explicitly support it.

The common interface follows the practical principle that data should be addressable through file-like operations. It does not pretend all platforms have identical metadata. Each snapshot records a filesystem-semantics profile and a capability matrix.

### 5.2 Path component preservation

A namespace entry records:

- Raw source path component bytes or a lossless platform encoding.
- Safe display form.
- Parent entry reference.
- Source comparison and case-sensitivity semantics.
- Unicode normalization behavior when known.
- Entry class and stable source identity when available.

Portable records MUST NOT identify an entry only by a normalized slash-separated string. Path lookup is component-by-component under the recorded source semantics. Adapters return opaque `path_ref` values so callers do not need to reproduce platform-specific comparison rules.

Absolute host paths, `..`, embedded separators, invalid encodings, device aliases, and symbolic links are never concatenated into an unrestricted host path during resolution.

The qualified Linux FUSE profile projects representable raw component bytes directly, including non-UTF-8 names. It MUST NOT normalize, case-fold, transcode, or silently escape a name. NUL, slash, an unresolvable collision, or another component that cannot be represented faithfully on the selected mount profile makes that entry or export explicitly unsupported rather than causing a renamed path to appear.

### 5.3 NamespaceRoot

One `NamespaceRoot` binds:

- Snapshot and namespace generation.
- Source and capture references.
- Root entry reference.
- Filesystem-semantics and metadata-capability profiles.
- Entry and child-index root digests.
- Required schema versions.
- Coverage summary and unresolved observation failures.

The root is included in the signed RRF closure. Repository object names and operational database identifiers are excluded.

### 5.4 NamespaceEntry

One `NamespaceEntry` records at least:

- Entry reference and parent reference.
- Raw and display path component.
- Entry class.
- File-version reference when applicable.
- Directory child-index reference when applicable.
- Symbolic-link target bytes when applicable.
- Hard-link group reference when applicable.
- Recorded permissions, ownership, timestamps, flags, and platform metadata.
- ACL, extended-attribute, named-stream, and sparse-layout attachment references when present.
- Observation state, errors, and fidelity limitations.

Empty directories are explicit entries. A repository's omission of empty directories cannot erase them from the logical namespace.

## 6. File, content, and representation model

### 6.1 FileVersion

A `FileVersion` identifies one recorded file state. It binds:

- Exact logical length.
- Exact content reference when readable and admitted to the exact lane.
- Metadata and filesystem-semantics references.
- Sparse extent map where applicable.
- Hard-link group semantics.
- Available representation references.
- Required exact representation and recovery contract.

Two paths may refer to the same file version. Two file versions may refer to the same exact content while carrying different names or metadata. Content deduplication therefore does not merge their namespace or metadata identities.

### 6.2 ContentRef

An exact `ContentRef` contains an algorithm-qualified digest and length. The reference distribution uses a required cryptographic digest over the logical file byte stream. Repository-private chunk digests and perceptual fingerprints are different identities and MUST use different typed fields.

For a sparse file, the logical content digest covers the byte stream a conforming read returns, including logical zero regions. The sparse layout is recorded separately so a restore can recreate holes rather than merely writing zeros when supported.

### 6.3 RepresentationRecord

A `RepresentationRecord` states how to materialize or validate a subject. It records:

- Representation reference.
- Representation kind: `EXACT_RAW`, `EXACT_REVERSIBLE`, `APPROXIMATE`, or `DERIVED`.
- Recovery-claim reference using a subject, relation, protected-component scope, validator profile, and policy authority. A discovery-only artifact records its non-recovery disposition instead of inventing a restore claim.
- Lifecycle class: `AUTHORITATIVE_DATA`, `RECOVERABLE_REPRESENTATION`, `REBUILDABLE_DERIVATIVE`, or `EPHEMERAL_CACHE`.
- Source subject, file-version, and content references.
- Producer, algorithm, model, codec, version, parameters, and implementation digest.
- Losslessness, fidelity, determinism, and validation evidence supporting the declared kind and recovery claim.
- Decoder or retrieval dependency graph.
- Expected output content identity or validator contract.
- Required keys or credentials as opaque references only.
- Placement references and availability state.
- Verification evidence and supported access modes.

These axes are independent. Representation kind describes the form, recovery claim describes the outcome it may satisfy after validation, and lifecycle describes retention and rebuild authority. Source provenance belongs in source-subject and source-binding records; rebuildability belongs in dependencies and lifecycle. Normalized, perceptual, generative, semantic, preview, and similar labels are transformation-purpose metadata rather than additional representation kinds or lifecycle classes.

Initial exact representations are repository-managed `EXACT_RAW` or independently verified `EXACT_REVERSIBLE` forms and may be chunked, compressed, encrypted, and packed. Later representations may include normalized documents, previews, transcoded media, parity, neural encodings, or externally retrievable objects; their kind and recovery claim depend on their intended recovery use and validation evidence. A reacquisition recipe remains a source binding until acquired bytes are independently validated, admitted, and placed as a representation.

### 6.4 Extents and composite representations

A logical file may be materialized from multiple extents. Each extent records:

- Logical offset and length.
- Source representation and representation offset.
- Zero, data, inline, external, or reconstruction kind.
- Expected digest when independently verifiable.

Extents support sparse files, chunked repositories, composite archives, and future erasure-coded or transformed layouts. The core validates complete non-overlapping coverage for an exact file before publication.

### 6.5 PlacementRef

A `PlacementRef` binds one representation to one repository receipt. It records repository identity, immutable locator, placement role, operation attempt, capability revision, generation, fence, and reconciliation evidence.

A placement may contain data for many files, and one file may depend on many placement objects. Neither fact appears in the user-visible path hierarchy.

## 7. Physical storage mapped to the original tree

Consider this source tree:

~~~text
/Media/song.flac
/Projects/demo/assets/theme.flac
/Documents/report.pdf
~~~

The two audio paths may contain identical bytes. A repository may store one set of encrypted chunks inside several pack files, while the PDF shares chunks with an earlier snapshot. The logical mapping remains:

~~~text
snapshot
├── Media/song.flac
│   └── FileVersion A -> Content X -> Exact Representation R1
├── Projects/demo/assets/theme.flac
│   └── FileVersion B -> Content X -> Exact Representation R1
└── Documents/report.pdf
    └── FileVersion C -> Content Y -> Exact Representation R2
~~~

`R1` may resolve to chunk ranges inside repository packs. That physical detail is private. Listing either directory returns its own original entry. Restoring both audio paths recreates two paths and their recorded metadata even though their exact bytes were stored once.

A later compressed or perceptual audio representation may be attached to Content X as `R3`. It is discoverable as an alternative representation, but an exact read continues to select `R1`.

## 8. SnapshotTree contract

`SnapshotTree` is a stable read-only semantic interface:

~~~go
type SnapshotTree interface {
    Root(context.Context, SnapshotRef) (PathRef, error)
    Resolve(context.Context, PathRef, []PathComponent) (PathRef, error)
    Stat(context.Context, PathRef) (EntryStat, error)
    List(context.Context, PathRef, PageToken, uint32) (EntryPage, error)
    Readlink(context.Context, PathRef) ([]byte, error)
}
~~~

The Go shape is illustrative and not a public package promise. Stable behavior includes:

- Every view is pinned to one committed snapshot and namespace root.
- Results use opaque references plus safe display fields.
- Directory pagination has stable ordering within the pinned view.
- `Resolve` does not follow symbolic links by default.
- `Readlink` returns the recorded target without resolving it against the host.
- Hard-link group and sparse-layout information are inspectable.
- An excluded or failed entry is visible with its state but cannot be presented as recoverable content.

### 8.1 PageToken semantics

A `PageToken` is an opaque, authenticated continuation capability issued by the core. It binds at least:

- Interface major version, workspace, principal, and authorization-policy revision.
- Exact snapshot, namespace-root digest, and parent `PathRef`.
- Sort, filtering, projection, and traversal semantics.
- The next position in the immutable child ordering, issuance time, and expiry.

A token is valid only for the exact listing that issued it. It cannot be replayed against another principal, directory, snapshot, namespace generation, sort, filter, or authorization state. A successful retry of an identical request may return the identical page, but it MUST NOT advance to a different page; continuation is otherwise a forward-only chain. Following one chain over an immutable directory returns every authorized child exactly once with no duplication or omission. Invalid, expired, superseded, or out-of-scope tokens fail with a typed reason and never fall back to an unpinned listing.

## 9. FileAccess contract

`FileAccess` authorizes and opens one representation for one immutable file version:

~~~go
type FileAccess interface {
    Open(context.Context, OpenRequest) (ContentHandle, error)
    Read(context.Context, ContentHandle, uint64, uint32) (ReadChunk, error)
    Close(context.Context, ContentHandle) error
}
~~~

An `OpenRequest` includes:

- Snapshot and path reference.
- Requested access contract, defaulting to `EXACT`.
- Optional explicit representation reference.
- Expected file-version or content reference for optimistic consistency.
- Read budget and intended access mode.

The core performs:

1. Snapshot and subject authorization.
2. File-version resolution.
3. Representation selection under the requested contract.
4. Placement-health and decoder-dependency checks.
5. Creation of a principal-bound, expiring read handle.

The read handle does not expose repository credentials or unrestricted storage locators. It cannot be retargeted to another subject, representation, range budget, or principal.

### 9.1 Exact selection

For an exact request, the selected representation must:

- Be `EXACT_RAW` or `EXACT_REVERSIBLE` and carry an accepted exact recovery claim.
- Have a complete decoder and placement closure.
- Produce the recorded length.
- Validate against the exact `ContentRef` at the required verification scope.

If no accepted exact representation is available, the open fails with a typed integrity or availability reason. The service does not silently return the nearest semantic result.

### 9.2 Bounded range reads

Range reads enforce:

- Maximum bytes per read and per session.
- Maximum session lifetime and idle time.
- Maximum concurrent sessions per principal and workspace.
- Cancellation and deadline propagation.
- Decoder expansion limits.

A range read verifies only the bytes, chunks, and decoder steps covered by that operation. It does not upgrade the whole file or snapshot to full-byte verified.

## 10. Restore semantics

Restore is a planned mutation, not an unrestricted recursive copy.

An exact restore flow is:

1. Pin the committed snapshot, namespace root, path selection, and exact representations.
2. Preflight destination capabilities, capacity, conflicts, and allowed root.
3. Create an immutable restore plan and digest.
4. Stage directories, files, links, and metadata outside the final destination where possible.
5. Stream selected representations through `FileAccess` or an equivalent internal read path.
6. Verify complete content identities and required metadata outcomes.
7. Atomically publish or finalize the destination according to its filesystem capabilities.
8. Emit a `RestoreResult` listing exact success, declared degradation, skipped unsupported semantics, and failures.

Restore ordering protects against path traversal and link attacks. Directory metadata is normally applied after children. Hard links are recreated only within the authorized restore root. Symbolic-link targets are written as recorded bytes and are not followed while restoring descendants.

When a target cannot reproduce source metadata, the plan declares the difference in advance. Policy decides whether that difference blocks, degrades, or is acceptable. Exact byte success and exact filesystem-semantics success remain separate claims.

## 11. Incremental namespace generations

### 11.1 Successor snapshots

Each ingest publishes a new immutable namespace generation. The planner compares it with a selected base snapshot and may reuse:

- Unchanged namespace subtrees by digest.
- Existing file-version and content records.
- Accepted representations and placements.
- Previously produced derivatives whose complete input and producer identity still match.

A renamed file receives a new path binding. Its content and representation may be reused. A metadata change produces a new file version even when exact content is unchanged. A processor or codec upgrade creates a new derived representation without changing the historical file version.

### 11.2 Source deletions

If an entry is absent from the new qualified capture, it is absent from the successor namespace. Historical snapshots retain their entries until retention retires those snapshots.

For operator clarity, an incremental change record classifies:

- Added path.
- Removed path.
- Renamed or moved path when evidence supports the relation.
- Content changed.
- Metadata changed.
- Type changed.
- Observation uncertain or failed.

Uncertain observation is not treated as confirmed deletion.

### 11.3 Current views

An adapter may expose a configured latest committed snapshot as a convenient current view. That pointer is a rebuildable projection. It never mutates an older namespace or becomes publication authority.

## 12. Retention and garbage collection

Snapshot retirement removes a snapshot from an active retention set after policy and authority checks. It does not immediately erase its records or physical bytes.

Physical GC requires a reachability analysis over:

- Retained committed snapshots.
- Required `PREPARED_CLOSURE` and `PUBLICATION_COMMIT` objects.
- Active restore, read, export, migration, repair, and verification leases.
- Active plans and in-flight publications.
- Pinned representations, retention locks, and supported holds.

The resulting immutable candidate plan identifies representations and placements that are no longer reachable. A qualified `RepositoryDriver` performs physical collection under a lease and fence, then returns a reconciled receipt. RestoreWeave rechecks retained snapshot health after destructive collection.

Index rows, thumbnails, embeddings, and caches have their own derivative retention. Their deletion cannot substitute for repository GC, and their continued presence cannot make retired authoritative bytes recoverable.

## 13. Search and semantic access

Search is a projection over authoritative subjects.

### 13.1 Index inputs

An authorized replayable index feed may include:

- Path, name, type, size, timestamps, and declared metadata.
- Extracted text and structured fields.
- User-authored tags, notes, and accepted corrections when supported.
- OCR, transcripts, captions, fingerprints, thumbnails, and previews.
- CLIP or other embeddings produced by an external processor.
- Snapshot generation, `SubjectRef`, producer provenance, and indexability policy.

Every derived value binds its complete input and producer revision. Replacing an embedding model creates a new model space; vectors from incompatible spaces are not merged as if directly comparable.

### 13.2 Query results

The host query broker queries exactly one explicitly named `IndexGenerationRef` per invocation. It validates provider and generation compatibility before invoking the `QueryProvider`.

A `QueryProvider` returns ranked candidates with:

- `SubjectRef` and optional `path_ref`.
- Indexed snapshot or RRF revision.
- Score and score semantics.
- Matched field or derivative reference.
- Provider and ranking revision.
- Staleness and completeness indicators.

Before a presentation adapter receives a result, the host query broker resolves the candidate through `SnapshotTree` and reapplies current authorization. Content access then uses `FileAccess`. A stale index hit may be reported as stale or omitted; it cannot resurrect a retired permission or bypass path authorization.

### 13.3 Query and browse coexistence

Browse answers, "What is at this exact path in this snapshot?" Search answers, "Which subjects may be relevant?" Both are product features, but only browse and the RRF namespace define recovery truth.

## 14. Portable publication and clean recovery

Every published snapshot binds its namespace root and representation graph into the signed RRF root. The portable protocol uses:

- Reconciled `PAYLOAD` placement receipts.
- A reconciled `PREPARED_CLOSURE` containing the authenticated RRF root and clean-recovery dependencies.
- A reconciled `PUBLICATION_COMMIT` containing the signed commit record that binds the root, payload receipts, prepared closure, plan and capture digests, verification gate, generation, and fence.

Only a valid `PUBLICATION_COMMIT` establishes logical publication. Local current-snapshot pointers, SQLite rows, gateway caches, and search indexes are projections.

A clean recovery process:

1. Enumerates only candidates tagged as RestoreWeave publication commits.
2. Authenticates the selected commit against an independently supplied trust anchor.
3. Verifies every bound receipt and digest.
4. Loads the exact prepared closure and required schemas or readers.
5. Reconstructs the namespace root and representation mappings.
6. Exposes the same `SnapshotTree` and `FileAccess` semantics available on the original host.

An orphan payload, orphan prepared closure, unsigned record, or locally cached key that is not independently trusted cannot establish publication.

## 15. Operational projection

SQLite or another local database MAY project:

- Snapshot and source lists.
- Parent-child namespace indexes.
- Path lookup accelerators.
- File-version, content, representation, and placement joins.
- Job state, read leases, and verification summaries.
- Index-feed checkpoints and query-provider health.
- Incremental change summaries and retention candidates.

The projection is private. It can be deleted and rebuilt from RRF records, portable publication evidence, and the durable journal. Projection rebuild interruption leaves the previous published snapshot authoritative and resumes from a checkpoint.

No CLI, MCP, REST, gateway, processor, index provider, or query provider may query private tables as its compatibility contract.

## 16. Gateway adapters

The reference distribution bundles a read-only Linux FUSE adapter over `SnapshotTree` and `FileAccess`. SMB, NFS, WebDAV, S3-compatible, media-server, alternate FUSE, and other NAS presentations are later adapters over the same contracts. Gateways are presentation adapters, not new storage-algorithm extension seams.

Each exported view is pinned to:

- Workspace and principal.
- Snapshot or explicitly configured current committed pointer.
- Export root.
- Allowed operations and content budget.
- Expiry and revocation state.

Gateways do not receive global repository credentials or policy authority. A gateway cache validates snapshot, file-version, representation, range, and verification identity before reuse.

The bundled FUSE profile binds exactly one principal, one export root, and one immutable snapshot per mount. A configured `latest` selector is resolved before mount publication and becomes that exact snapshot; it is never a moving view. The adapter verifies the effective mount flags and requires `ro,nodev,nosuid,noexec`; it refuses `allow_other` and arbitrary mount-option passthrough.

The bundled FUSE adapter:

- Resolves `latest` to one exact committed snapshot when the mount opens; it never changes the mounted generation silently.
- Keeps every open handle pinned to its file version and representation until close.
- Supports concurrent directory and regular-file handles, random and sequential reads, symbolic-link target reads, and collision-resolved stable inode mapping for the mount lifetime.
- Returns typed I/O failures for unavailable, corrupt, unauthorized, or non-exact content and never substitutes a similar representation.
- Reports only the verification state actually known for the bytes read.
- Rejects every write-capable open and every mutation opcode, including create, write, truncate, allocate, rename, link, symlink, unlink, directory mutation, ownership, mode, timestamp, xattr, ACL, and device-node mutation, with `EROFS` before any side effect.

### 16.1 Inodes, directory cookies, and filesystem fidelity

The adapter owns a mount-local inode table. The table maps the authenticated root and entry identity to a nonzero kernel inode, detects numeric collisions, and resolves them without aliasing unrelated entries. Members of one recorded hard-link group map to the same inode and link count; entries outside that group MUST NOT share an inode merely because a hash collides. Identity remains stable for the mount lifetime but is not a portable record or a promise across mounts.

FUSE directory offsets are opaque handle-local cookies, not child ordinals, byte offsets, database row IDs, or durable `PageToken` values. The adapter translates each cookie through the exact `SnapshotTree` continuation state for that directory handle. A cookie cannot be replayed across handles, directories, principals, snapshots, or mounts. Restarting from offset zero produces the same immutable ordering, and continuing from a returned cookie neither duplicates nor omits an entry. `READDIRPLUS` is enabled only for a Linux compatibility tuple whose large-directory scaling, lookup amplification, memory use, inode stability, and cache behavior have been qualified; otherwise the adapter uses ordinary `readdir`.

Sparse-file reads return zero bytes for recorded holes without materializing the entire logical file. Logical size always matches `FileVersion`. Reported block allocation and any `SEEK_DATA` or `SEEK_HOLE` support derive from the recorded sparse-layout profile, never from repository pack allocation or deduplication; unsupported seek semantics fail explicitly rather than inventing extents. Raw name bytes, symbolic-link target bytes, hard-link membership, and declared metadata follow the same authenticated records used by `SnapshotTree`.

### 16.2 Caches, authorization, and teardown

Attribute, entry, negative-entry, and data-cache behavior is part of the qualified mount profile. Cache keys bind the immutable snapshot, namespace root, entry or file version, representation, and verification identity. TTLs and memory bounds are explicit, and an authorization or integrity transition invalidates userspace caches and initiates unmount or revocation handling.

Kernel page cache, already-open file descriptors, pending requests, and `mmap` can outlive a userspace authorization check. Qualification therefore measures residual access after principal or mount authorization expires or is revoked, including access through existing handles, cached pages, new page faults, and mappings. If the implementation cannot enforce the declared revocation bound, documentation and status MUST label the mount a local-trust surface rather than claiming strong multi-user revocation. Clean unmount and daemon-crash teardown must not leave a writable, silently repointed, or falsely healthy view.

A future writable namespace requires a separate transaction, conflict, durability, and capture design and is not implied by this read contract.

## 17. Security and failure behavior

- Path resolution is component-based and fail-closed.
- Symbolic links are data unless a caller explicitly requests a separately authorized follow operation.
- Directory listings and search results are filtered before disclosure.
- Content handles are principal-bound, representation-bound, range-limited, and expiring.
- Decoder processes receive bounded input and output handles, deadlines, resource limits, and no ambient network or secrets.
- Compression bombs, nested-container expansion, and malformed media are bounded processor failures, not host-memory authority.
- Repository timeouts enter reconciliation; they do not become successful reads or deletes by assumption.
- A missing index degrades intelligent search but not exact browse or restore.
- A missing optional derived representation falls back only when the requested access contract permits it. Exact requests fail rather than substitute.
- Any corrupt namespace, representation, placement, or commit binding produces an integrity failure with the affected subject and snapshot identified.

## 18. Acceptance tests

### NA-AT-001: Packed storage round trip

Ingest a tree whose files are deduplicated and stored inside shared repository packs. Browse and restore the exact original directory structure without exposing pack paths.

### NA-AT-002: Duplicate content, distinct paths

Two paths with identical bytes share content and representation identity while retaining distinct entry, metadata, and restore behavior.

### NA-AT-003: Exact default

Attach a lossy media representation and an exact representation to one file. An unspecified representation read returns exact bytes; loss of the exact placement produces a typed failure rather than the lossy output.

### NA-AT-004: Cross-platform capture profile

Run the namespace conformance corpus through at least two platform profiles. Platform-specific metadata differences are declared, and no platform-specific field is required by the common read contract.

### NA-AT-005: Incremental reuse

Publish a successor snapshot with one rename, one metadata-only edit, one content edit, one addition, and one deletion. Unchanged content is reused, both historical trees remain browseable, and uncertain observation is not classified as deletion.

### NA-AT-006: Portable recovery

Delete SQLite and every search index. From a valid portable commit and independent trust anchor, reconstruct the namespace and restore selected exact files.

### NA-AT-007: Orphan closure rejection

An available payload or prepared closure without a valid bound publication commit is not listed as a published snapshot.

### NA-AT-008: Path safety

Malicious components, symbolic links, case collisions, and Unicode normalization differences cannot escape the authenticated namespace or restore destination.

### NA-AT-009: Sparse and hard-link fidelity

Exact logical bytes verify, supported targets recreate sparse regions and hard-link groups, and unsupported targets report precise degradation.

### NA-AT-010: Honest partial verification

A successful range read records only the checked range. It does not mark the whole file or snapshot full-byte verified.

### NA-AT-011: Search independence

Rebuild the full-text or vector index with a different implementation. Snapshot identity, exact browse, reads, and restore results remain unchanged.

### NA-AT-012: Query authorization

A query provider returns a stale or unauthorized subject candidate. The core query broker suppresses or marks the result after authoritative resolution, and no presentation adapter can open bytes without `FileAccess` authorization.

### NA-AT-013: GC reachability

A GC plan cannot select placements reachable from retained snapshots, portable closures, or active read and restore leases.

### NA-AT-014: CLI and MCP equivalence

For the same principal and content handle budget, CLI JSON and read-only MCP expose the same path metadata, content identity, representation identity, bytes, and typed errors.

### NA-AT-015: Bundled Linux FUSE equivalence

Mount one committed snapshot for one principal and export root with verified `ro,nodev,nosuid,noexec` flags and no `allow_other`. Compare directory listings, raw names, attributes, symbolic links, hard links, sparse extents, random ranges, streamed files, and failure cases with `SnapshotTree` and `FileAccess`. Open handles remain generation-pinned; unrelated entries never alias after inode collisions; directory cookies cannot cross handles or scopes; cache reuse validates identity; and every write-capable open and mutation opcode returns `EROFS` without side effects. Exercise authorization expiry and revocation through new opens, existing handles, page cache, and `mmap`, and report any residual access beyond the declared bound as a local-trust limitation.

### NA-AT-016: Pagination and directory-cookie continuity

Traverse large immutable directories through CLI, MCP, `SnapshotTree`, and FUSE using varied page sizes, retries, concurrent directory handles, and seek-back-to-zero behavior. Each scoped continuation chain returns every authorized entry exactly once. Tokens and cookies fail when replayed across a principal, directory, snapshot, sort, authorization revision, handle, or mount, and a retried identical page never changes its contents.

### NA-AT-017: Linux FUSE performance qualification

For the declared repository, kernel, adapter, cache, and mount-policy tuple, publish cold and warm first-byte latency; large-directory `readdir` and qualified `READDIRPLUS` scaling; sequential and random-read throughput; repository request and byte amplification; process and kernel-cache memory; concurrent file and directory handle limits; attribute, entry, negative-entry, and data-cache behavior; revocation residual access; and clean and crash-driven unmount behavior. A regression outside the declared envelope cannot be hidden by increasing cache lifetime, weakening authorization checks, changing exact-byte selection, or silently disabling integrity verification.

## 19. Implementation decision

RestoreWeave will treat the authenticated file-shaped namespace, not a repository tree or search index, as the stable access standard. Repository engines are free to optimize physical storage aggressively. Processors are free to add better representations. Indexes are free to improve discovery. The core preserves the mapping that turns all of them back into an exact, verifiable, original directory view.
