# Remaining Work and Closed Decisions

> **Status:** Informative checkout record, 2026-08-20. The core implementation order is [Core MVP Execution Plan](core-mvp-execution-plan.md). Experience introduction path D1–D6 and fixture index dimensions remain historical interface checks, not core-release completion. This document does not add product requirements or select a release repository engine. **Occam:** do not add a subsystem when an existing surface plus a foreign tool already does the job.

Authority stays in the existing requirement set. Core sequencing stays in [Core MVP Execution Plan](core-mvp-execution-plan.md). Host qualification and later experience notes remain in [Implementation Completion Plan](implementation-completion-plan.md), [Experience Completion Plan](experience-completion-plan.md), and [Index Dimension Plan](index-dimension-plan.md).

## 0. What the product is

This plan follows [Content Store, Views, and Export Requirements](../requirements/content-store-views-and-exports.md). Older wording below that describes the original-path tree as the primary product surface is implementation history; tags, saved views, and frozen export manifests are the product organization model.

RestoreWeave is the **content and recovery plane**. It is not a NAS OS, not a media server, and not a client suite.

**Must have** (without these it is not RestoreWeave):

- Exact identity of bytes (`ContentIdentity` / SHA-256) and a stable `SubjectRef`.
- Ingest a tree without racing writers into a lie.
- An immutable snapshot + original-path recovery projection.
- Independent verify and restore that match SHA-256.
- A catalog that can find a subject through lexical, structured, tag/note, and default local semantic dimensions.
- Dynamic saved views and frozen reproducible export manifests.
- Durable annotations (`TAG` / `NOTE` / `PROGRESS`) that survive index rebuild and client loss.
- Bounded exact reads (`content.*` / `FileAccess`).
- Processors that may fail; they must not block exact ingest / verify / restore.

**Must keep** (delete the UI, the facade, the FTS file, even the SQLite catalog, and these still mean the same thing):

- The exact payload in the repository.
- The portable snapshot / publication.
- Subject identity and annotation export.
- The rule that a player library, search index, fingerprint, or embedding is never recovery authority.

**May have** (replaceable, optional, later — not the definition):

- A real repository engine (after readback gates). Isolated heavy parsers. Inbox page. OpenSubsonic / OPDS HTTP APIs. CLI / MCP. Later video/books/games *catalog slices*. Foreign tools consuming a materialized export.

**Must not become:** writable NAS, embedded player/reader/media server, any built-in mount/SMB/WebDAV product, a second catalog inside a client, Tika/ffmpeg as identity.

## 1. What RestoreWeave owns

The core owns identity, admission, protection outcomes, snapshots, catalog facts, descriptions and annotations, index-generation truth, query fusion, plans, repository/publication evidence, `FileAccess`, saved views, export manifests, verification, and restore.

Those surfaces hand out **exact bytes and stable IDs**. Other programs may play, read, share, or mount from there.

**ABI vs adapters.** The Unix-socket command envelope is the daemon's internal contract. `rw` is the required operator client. The existing Inbox, OpenSubsonic, and OPDS surfaces are optional compatibility experiments over that ABI, frozen to maintenance-only until the core execution plan closes. They do not define the public core, recovery authority, or release completion, and no second REST catalog is added.

## 2. Closed decisions — do not reopen

| Decision | Meaning | Common mistake |
| --- | --- | --- |
| **Occam's razor** | Do not add a mount server, WebDAV, SMB, player, or second catalog. No mount operation exists. | “We should make it mountable ourselves” |
| **No RestoreWeave mount product** | We do not ship, qualify, or expose a kernel-filesystem server. The mount ABI, CLI commands, package, and dependency are removed. | Treat any platform mount as remaining core work |
| **Foreign tools mount** | Operators who want a folder use rclone, sshfs, NAS SMB, WebDAV helpers, or similar against a **restored directory** or exact read bytes | Build WebDAV/SMB/FUSE inside `restoreweaved` so “it can mount” |
| **Two proofs stay separate** | Recovery = ingest → verify → restore SHA-256. Product = one Inbox search, one open, one restore | “A client can play” or “Navidrome scanned a folder” as either proof |
| **Adapters, not products** | Existing clients attach to HTTP APIs. We adapt OpenSubsonic/OPDS to our daemon. We do not vendor those apps. | Fork Feishin/Navidrome/KOReader; invent a RestoreWeave player; publish the Unix socket as the app SDK |
| **Constrained clients** | Only `rw`, read-only MCP, one Inbox page, plus foreign OpenSubsonic/OPDS apps. No native app, no full WebUI, no second catalog API. | A RestoreWeave iOS/Android/Vue suite |
| **Candidate is not the release engine** | The raw directory CAS is for development; `local-zstd-v1` is a single-machine measurement candidate. Restic/Kopia CLI green or zstd unit tests do not select a release engine | Rename a tested candidate “production” before encryption, GC, repair, failure, migration, and reader-closure gates pass |
| **Local semantic search is the default** | ONNX Runtime + pinned BGE profile + in-process zvec; no Compose dependency | Treat fixture embeddings as completion, or require Qdrant/Milvus for personal use |
| **Views are not exports** | Saved views are dynamic; `ExportManifest` freezes membership and representations | Restore a live query and call it reproducible |
| **Darwin ≠ missing POSIX** | This Mac can run catalog, CLI, sockets, Inbox. It cannot close Linux namespace gates | Use `sandbox-exec` as bubblewrap; call Darwin “not Unix” |
| **Default ingest stays in-process** | Processor failure must not block exact ingest / verify / restore | Make Tika/ffmpeg/ffprobe core identity |
| **Facade maintenance-only freeze** | Existing Inbox/OpenSubsonic/OPDS code may receive correctness/security maintenance only until core phases close | Add endpoints or count adapter handshakes as product progress |
| **No OpenList core** | OpenList may be an external tool or research reference; it is not a dependency, storage engine, catalog authority, or fork base | Rebuild RestoreWeave around a path-first file-manager model |
| **Do not expand core scope** | Clarify existing contracts and record status without adding product families | Add FUSE, WebUI, players, new dimensions, or client products because they feel missing |

## 3. What this checkout already has

- Exact ingest / verify / restore over both the development raw directory CAS and the opt-in `local-zstd-v1` candidate. The latter is embedded, requires no Compose service, performs whole-file SHA-256 deduplication, stores checksummed zstd frames, reports logical and physical placement bytes separately, rejects raw/zstd profile mismatches, and passes catalog-free signed restore. `snapshot.verify` accepts `authenticated-metadata`, `sampled-content`, `full-bytes` (default), `restore-drill`, and `clean-recovery`. Only restore-drill may report `restore_verified`; neither in-tree profile is a qualified release repository.
- Strict persisted config plus per-file `STORE_EXACT`, `STORE_EXACT_WITH_EXTERNAL_FALLBACK`, `LINK_ONLY`, and `METADATA_ONLY` overrides: repeated path-scoped locators, expected SHA-256/length, raw names and metadata, typed planned outcomes/reasons, a protection digest, and portable locator records. Link-only writes no CAS payload. Unresolved readable content is retained exactly and recorded as `EXACT_FALLBACK`. Failed/unstable entries remain visible with their requested mode, `BLOCKED`/`UNAVAILABLE` outcome, and typed scanner reason in a non-executable plan. A successor can explicitly resolve only a stable rooted regular-file read failure as metadata-only; it cannot resolve instability, path/boundary uncertainty, non-files, cancelled scans, or failed scans. External retrieval does not exist yet.
- `annotation.import` conflict policy: `fail` (default), `keep-local`, `keep-imported`. `rw annotation import --conflict` and Inbox import use the same field.
- Command ABI, CLI, read-only MCP.
- Loopback facade: OpenSubsonic (client-viable methods, CORS, `enc:` / salt-token auth, honest empty handshake methods), OPDS (search, pagination, acquire, JSON progress), Inbox (`/inbox`).
- In-process EXTRACT for UTF-8 text, ID3/FLAC/OGG tags, EPUB OPF.
- Portable source stat (mtime/uid/gid) on namespace records.
- Signed publication closure foundation: the default daemon signs immutable `PREPARED_CLOSURE` and `PUBLICATION_COMMIT` records with Ed25519, records payload aggregate receipts and authenticated metadata evidence, discovers only committed snapshots, and can reconcile a missing SQLite projection from a committed repository record. Catalog-free list, verify, diff, restore planning, and restore use the signed commit path. After optional processing, a separate signed `PROCESSOR_ATTEMPT_CLOSURE` authenticates the deterministic terminal-attempt bundle against the fully validated parent commit; its catalog-free reader rejects tampering, a missing parent, noncanonical fields, and conflicting replay without affecting exact restore. A catalog-free recovery-reader daemon now validates v2 references against an independently retained trust anchor and read-only repository, and imports v1 recovery bundles without SQLite, indexes, or signing material. `recovery.import` verifies a bundle against a supplied anchor (catalog-free or reconciling a provided store), and `recovery.token.export` emits deterministic `recovery-token.v1` proof envelopes for subjects with a recovery reference (metadata-only subjects fail closed with no recovery path). `snapshot.v2` authenticates typed hard-link, sparse-indication, boundary, and detection facts plus explicit unsupported extended-metadata states; its base evidence is deliberately marked `PARTIAL` and lists the remaining omissions. Publication is now fenced across processes through the `publication_fences` projection: an acquired lease stamps a monotonic fencing token into every signed record and is validated before each placement, with an explicit seam keeping catalog-free readers on `FenceToken: 1`. The portable-fact child is validated against its parent manifest on the catalog-free read path. The implementation has one repository; it has no cross-process fencing for restore-destination writes and no general unknown-outcome reconciliation beyond plan-level ingestion/restore. Portable processor artifact bodies and subject mapping, retry/successor lineage, descriptions/annotations, per-field name/ownership/mode/time provenance, actual qualified extended-metadata/extent capture, and portable link-only locator/reference qualification remain open. Executing external reacquisition remains a later `RetrieverDriver` profile, not an MVP closure gate.
- `recovery.export` writes a new authenticated bundle containing the selected publication commit and prepared closure (and refuses overwrite); it does not export private signing material. The clean-install reader admits that v1 bundle through the recovery socket after independently validating the v2 reference and trust anchor. Full cross-platform qualification and an independent failure-domain/release claim remain open. The public trust anchor is exported separately and must be retained independently.
- `snapshot.diff` compares two repository manifests by original path (added / removed / moved / content / metadata / type). Catalog-free.
- `namespace.resolve` walks display-name components to a catalog entry id and does not follow symbolic links.
- `representation.list` reports catalog representations for one subject or file version. It does not open content. Placement is `unknown` without the exact lane, and SHA-256 verified when a configured repository profile is present.
- `status.get` reports publication count, plan/job counts, recent plan and job summaries, and, with the exact lane, repository path plus snapshot count. It also reaps expired `content.*` handles (15-minute idle TTL).
- `plan.ingest` performs read-only inspection and records a `READY` immutable ingest plan with estimates, config digest, and source basis. `plan.get` reads it. `plan.apply` revalidates the digest and source before publishing; a repeated apply replays the same logical result. A committed publication is keyed by plan digest, so an expired apply lease can reconcile the same snapshot instead of publishing again. Processor or index failure after publication returns `DEGRADED`, while the exact apply Job and plan remain successful.
- Post-publication processor failures are returned as a bounded per-subject warning list containing subject, display name, stage, capability, status, and reason. `LINK_ONLY` and `METADATA_ONLY` subjects without local payload are skipped rather than reported as processor failures. Every routed capability receives an immutable terminal attempt row; a deterministic JSON projection is bound into a signed post-commit child. Retry/reconciliation scheduling, signed successor lineage, portable artifact bodies, and portable subject mapping remain open.
- `plan.restore` always performs a read-only preflight and records an immutable restore plan. It never creates the destination or writes CAS bytes. A plan is executable only when it includes a concrete empty destination; `plan.apply` revalidates the manifest and destination basis before writing it. After a worker crash, only a complete non-empty destination is eligible for reconciliation, and only after exact path-set, type, file-length, SHA-256, and symlink-target validation; partial, changed, or extra output fails closed. Empty snapshots have no output evidence and are outside this reconciliation claim. A link-only or metadata-only snapshot fails preflight because it has no local exact recovery.
- `plan.revise` inserts an immutable successor, re-inspects the source, and recomputes supported ingest-protection consequences without editing the base plan. A successor remains non-executable when blocked source entries remain. `plan.abandon` inserts an abandonment marker for one unapplied successor; it refuses plans whose apply has started and does not delete snapshots. Plan rows stay immutable.
- `doctor.check` reports catalog, identify, in-process processors, the active repository/compression profile, latest-snapshot SHA-256 readback, and an optional source path. It states that the active in-tree profile is not a selected release engine. `status.get` exposes the same profile tuple. Optional sandbox/bubblewrap is scoped and does not fail the Darwin catalog path. Inbox `/inbox/api/doctor` and read-only MCP expose the same report.
- Inbox `/inbox/api/status`, `/inbox/api/item`, `/inbox/api/job`, `/inbox/api/plan`, `/inbox/api/snapshots`, `/inbox/api/diff`, `/inbox/api/annotations`, `/inbox/api/resolve`, `/inbox/api/restore`, and `/inbox/api/recovery` call the same command ABI. GET `/inbox/api/annotations` exports; POST imports the same portable bundle. A restore POST without `destination` calls only `plan.restore` and returns `wrote=false`; with a destination it applies the returned workspace, plan id, and digest through `plan.apply`. Recovery export writes the signed commit/prepared bundle and returns 409 if the file already exists. Item provenance is a `namespace.stat` display-path walk plus `snapshot_ref`. Search also tries `namespace.resolve` on the query. No new public REST catalog.
- Read-only MCP now also exposes `snapshot.list`, `snapshot.diff`, `namespace.stat`, `namespace.readlink`, and bounded `content.open` / `content.read` / `content.close`. A content read returns a range SHA-256 and at most 256 bytes of untrusted preview, with a 4KiB per-call cap and a 64KiB session budget. It does not put unbounded base64 in the tool result. Full exact bytes stay on Inbox / OpenSubsonic / OPDS. MCP still does not expose ingest, restore, verify, cancel, or annotation mutation.
- D5 pin: this host’s Supersonic 0.22.1 method sequence is covered by `TestD5PinnedSupersonicCallSequence`. The written D5 exit is green. Pointing the installed GUI at the loopback facade is an operator check, not remaining product engineering. No KOReader.
- `plan.ingest` records a terminal `PLAN_INSPECT` job plus append-only `JOB_STARTED` / `JOB_SUCCEEDED` audit events. `plan.apply` records a digest-bound `PLAN_APPLY` job with the execution result. `job.events` pages those events. `job.cancel` cancels a queued apply job; a terminal job is already done and is not rolled back. This is not an async worker.
- Index dimension interfaces I1–I9 exist, but default readiness declares only providers that are actually wired. Lexical and the catalog-derived graph projection may be available; semantic, acoustic, and multimodal report unavailable by default, with semantic notes including `SEMANTIC_INDEX_UNAVAILABLE`. `search.query` can name one dimension or `fuse` two or more available dimensions; the broker keeps per-component provenance. Fixture processors (`fingerprint.audio.fixture.v1`, `embed.text.fixture.v1`, `embed.clip.fixture.v1`) require the explicit qualification-harness option and are not SHA-256, Chromaprint, CLIP weights, ONNX, or zvec.
- Foreign storefronts (sticker/meme managers, 追番/美剧 libraries, quote vaults, and neighbors) are enumerated in [Foreign App Jobs](foreign-app-jobs.md). That note does not add products. Keep/find/restore stays on the plane; play/download/edit stays foreign.
- Text-first tool wedge: [Tool-Core Wedge Plan](tool-core-wedge-plan.md). Core stays ingest / identity / search / annotate / verify / restore. Processor and facade sockets stay; they are not filled in this checkout.

The D1–D5 adapter harness experiments are closed to feature work, and D6 is a closed non-goal. Their tests provide compatibility regression evidence only. `GOMODCACHE=… GOPROXY=off go test ./...` green on this Darwin host is **not** `RW-MVP-1` acceptance and **not** an engine selection.

## 4. What is actually left

Ordered by whether it is our job.

### Next, still our surface

| Item | Why it is still open | Done when |
| --- | --- | --- |
| **Honest repository** | Independent SHA-256 readback and `snapshot.verify` modes exist. `local-zstd-v1` proves embedded compression, whole-file dedup, no-replace placement, relocation, corruption rejection, profile isolation, and signed restore, but has no encryption, chunk dedup, reachability/GC, repair, complete space report, or production corpus | Use local zstd for single-machine corpus measurement; evaluate Kopia and any other mature candidate against one common GC-root, placement mapping, crash, corruption, relocation, credential, migration, clean-reader, packaging, support, and license gate. Select no engine before the dated qualification decision. Keep engine-private IDs out of portable identity |
| **Signed recovery closure** | Ed25519 prepared/commit records, payload receipts, authenticated metadata evidence, a signed post-commit terminal processor-attempt child, a signed portable-fact child with content-addressed bodies and subject mapping, catalog-free discovery/restore, export bundle, SQLite projection reconciliation, a catalog-free v2-reference/v1-bundle recovery reader, deterministic recovery tokens, and cross-process publication fencing exist. Actual platform-qualified xattr/ACL/extent capture, per-field name/ownership/mode/time provenance, processor retry/successor lineage, restore-destination cross-process fencing, general unknown-outcome reconciliation, and portable link-only references remain open. External reacquisition remains a later profile | Portable full recovery closure, qualified clean-install reader/import and independently supplied trust-anchor workflow, and failure/recovery tests for all admitted records |
| **`RW-MVP-1` acceptance** | Requirements exist; the release tuple does not. Non-mutating planning, digest-bound apply, ingest-publication and completed-restore reconciliation, typed ingest-protection revision, retained blocked-entry plans, narrow metadata-only operator resolution, durable processor-attempt outcomes, bounded per-subject processor warnings, plan get/abandon, `doctor.check`, terminal `job.events` / `job.cancel`, the signed closure foundation, cross-process publication fencing, clean-install import/reader, recovery tokens, saved views, and frozen export manifests exist. Processor retry/reconciliation, a formal async worker, the full signed recovery contract, and a release engine still do not | The existing MVP contract against a qualified repository, not a new FUSE or WebUI gate |
| **Default discovery** | The lexical/structured feed covers the complete baseline fields with typed filters, segment provenance, coverage reporting, and a fused lexical+graph broker. Fixture dimensions do not provide the required default semantic experience | Package and qualify ONNX Runtime, pinned BGE model/tokenizer, zvec v0.6.x generation, hybrid broker, rebuild, and degraded-mode behavior |
| **Views and exports** | Saved views (`rw view save/get/list/evaluate`) and frozen export manifests (`rw export plan/apply/verify`) are implemented and tested for the stated local scope | Release qualification and the fully ID-free ordinary user loop |

### Later, only if we take on heavier parsing

| Item | Why it can wait | Done when |
| --- | --- | --- |
| Isolated Tika / libarchive / ffprobe | Current in-process extracts already cover the catalog slice. Isolation is so a hostile file cannot own the host | Linux bubblewrap (or keep those processors out of the default pack). NOTICE/SBOM. LGPL-only FFmpeg if ffprobe ships. Cover-art extraction lives here, not as an in-process APIC parser |
| Additional model packs | Fixture acoustic/multimodal and the catalog-projected `graph-relation` generation exist. Chromaprint/CLIP weights are not in the default pack | Isolated Chromaprint/CLIP processors with ABI tests that still resolve to `SubjectRef`. The default local text embedding pack is handled above. Facades keep old standards |

### Not our job / not this slice

| Item | Disposition |
| --- | --- |
| Live FUSE / macFUSE / VM-for-FUSE / `allow_other` | Closed. Foreign mount tools only |
| D5 GUI click-through | Closed as a test method. Qualify a pinned client by reading its source and replaying its OpenSubsonic/OPDS calls in-process (`TestD5PinnedSupersonicCallSequence`). Do not drive the GUI. Do not write a Feishin substitute |
| KOReader private sync protocol | Not OPDS. Optional later experiment, not a second catalog |
| Player / reader / Jellyfin / video transcode | After `RW-MVP-1`. Never inside `restoreweaved` |
| Writable NAS, source retirement, Docker/installable release | Later named profiles |

## 5. Constrained client set

Do not grow a client platform. Each row is either ours and tiny, or someone else’s app.

| Surface | Kind | Allowed job | Forbidden |
| --- | --- | --- | --- |
| `rw` | Our CLI | Ingest, verify, restore, search, tags/notes | A TUI player/reader |
| `rw mcp` | Our MCP | Read-only inspect | Mutation, a second catalog |
| `/inbox` | One HTML page | Search, open, preview, verify, restore | Design system, accounts, writable NAS, File Browser |
| `/rest/*` | OpenSubsonic HTTP API | Existing music clients (Feishin, DSub, …) | Embed Navidrome; transcode |
| `/opds` | OPDS HTTP API | Existing readers (KOReader, …) | A RestoreWeave reader; KOReader private sync as a second catalog |
| Restored directory | Foreign tools | rclone, sshfs, NAS SMB | Our FUSE/WebDAV/SMB server |
| Native / full WebUI | None | — | Do not start |

Inbox is **not** a product WebUI. It is one embedded page that calls `/inbox/api/*`. Delete the page and CLI recovery still works.

## 6. Target operators

Typical users already have a Linux NAS or a KVM VPS. They do not need us to invent a disk. They need one catalog, exact recovery, and a way for **their** clients and **their** share tools to see bytes we already proved.

## 7. How the adapter is tested

Keep the loopback OpenSubsonic / OPDS facade. Old clients need a standard they already speak; RestoreWeave is new and must not become a second media server.

Qualify that facade against **both source trees**, not a GUI:

1. Read the pinned client’s request sequence (this host: Supersonic 0.22.1).
2. Replay those HTTP methods in-process (`TestD5PinnedSupersonicCallSequence` and `TestExperienceSurfacesOverCommandABI`).
3. Assert identity comes back as `SubjectRef` and stars/progress land in `annotation.list`.

Do not click Supersonic, Feishin, or KOReader to prove a phase. A human may still point a client at the facade; that is not remaining engineering.

Want a folder? `rw restore <snapshot-ref> <empty-dir>` creates a read-only restore plan. Review it, then apply the emitted `rw plan apply <plan-id> --workspace <workspace-id> --digest <plan-digest>` command; only that apply writes the directory. The operator can then use SMB/rclone/sshfs. Not `rw mount`.
