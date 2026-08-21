# Experience Completion Plan

> **Status:** Archived optional-adapter experiment, reclassified 2026-08-19. D1–D5 record interface and request-sequence harness coverage only; they are not a completed user experience, live-client qualification, or `RW-MVP-1` progress. D6 is a closed non-goal, not a completed feature. All adapter feature work is maintenance-only until [Core MVP Execution Plan](core-mvp-execution-plan.md) closes. This document adds no product requirement or release gate.

This file now preserves the completed adapter experiments and their limits. It is not an active implementation queue. Do not embed a media server, extend these adapters ahead of core work, or wait for live FUSE to prove the catalog.

## 1. What adapter evidence means

Two proofs stay separate.

| Proof | Complete when | Not complete when |
| --- | --- | --- |
| **Recovery** | CLI/MCP ingest → verify → restore matches SHA-256 without a UI | A client can play a file |
| **Product** | One Inbox search, one authorized open, one restore; specialized clients can attach without creating a second recovery authority | Three foreign UIs are still the weekly entry point |
| **Interop** | A current OpenSubsonic client and a current OPDS client can browse, fetch exact bytes, and write progress/stars back to `SubjectRef` | Navidrome can scan a FUSE mount |

A phase is done only when the listed exit test is green. “Looks like Subsonic” is not an exit.

## 2. Current checkout (2026-08-15)

The D1–D5 harness experiments are closed to new feature work. Their coded tests cover only the listed adapter contracts. D6 records the decision not to build FUSE. None closes the core user loop, semantic default, export workflow, or release repository gate.

Present:

- Command ABI: ingest, search, tags/notes, `PROGRESS`, `audio.list`, `books.list`, bounded `content.open`/`read`/`close`, verify, restore preflight and write, `doctor.check`, `plan.get`/`apply`/`revise`/`abandon`, `job.events`/`job.cancel`, `snapshot.list`/`diff`/`verify`, `recovery.export`, `namespace.resolve`/`stat`/`readlink`, `annotation.export`/`import`.
- Optional loopback facade (`restoreweaved --facade-listen`): OpenSubsonic client-viable methods; OPDS navigation/search/pagination/acquire; `POST /opds/progress` JSON write-back; thin Inbox at `/inbox` plus `/inbox/api/*` (status, search, item, preview, progress, verify, restore preflight/write, doctor, plan, job, snapshots, diff, annotations export/import, path resolve, recovery export).
- IDs on the facade are `SubjectRef`. The facade calls the command ABI only. No fourth public REST catalog.
- Inbox item page streams exact bytes and writes `PROGRESS`. Item JSON includes display path, `snapshot_ref`, representations, and annotations. Delete the page and CLI recovery still works.
- D5 pin: this host’s Supersonic 0.22.1 method sequence is harness-covered (`TestD5PinnedSupersonicCallSequence`). XML artist/album lists emit Subsonic attributes (`name="…"`).
- Namespace records carry source mtime/uid/gid. RestoreWeave does not own a FUSE server; foreign tools may mount a restored tree.
- Raw development CAS and the local-zstd candidate are the in-tree drivers. Release repository qualification is a separate gate, not an experience-path hole.

Operator-only, not remaining product engineering:

- Someone may point the installed Supersonic 0.22.1 GUI at the loopback facade and click login. The methods that GUI issues are already harness-green. Do not write a Feishin substitute.
- No KOReader/DSub install on this host. KOReader’s private progress-sync protocol is not implemented; D2 ships RestoreWeave JSON progress, not that protocol.

Later (not D1–D6):

- Isolated Tika/ffprobe, NAS/S3, engine selection, video/release packaging. Live FUSE is a closed non-goal, not an absence.

## 3. Long sequence

This archived sequence is not an active parallel workstream. Existing adapter regressions may be maintained, but no new experience feature work starts until the Core MVP Execution Plan permits it. Nothing waits for `/dev/fuse`, and this document cannot select or promote a release repository.

```text
D1  OpenSubsonic adapter contract              adapter-harness-complete; maintenance-only
D2  OPDS adapter contract                      adapter-harness-complete; maintenance-only
D3  Thin Inbox shell                           implemented; no drop/attach/import workflow
D4  Item-page preview/progress                 adapter-harness-complete; not a client product
D5  Pinned request-sequence replay             adapter-harness-complete; live client not qualified
D6  RestoreWeave FUSE                          closed non-goal
D7  Isolated processors and remaining repository gates   later
D8  Later video / release packaging            later
```

### D1 — Client-viable OpenSubsonic

Status (2026-08-19): **adapter-harness-complete; maintenance-only**. Implemented in `server/internal/gateway/protocol` and covered by `TestProtocolFacadeUsesCommandABI` plus `TestExperienceSurfacesOverCommandABI`. This is compatibility evidence, not a core product or live-client qualification.

**Why first.** Feishin, DSub, and similar clients fail closed on missing search, cover, user, and playlist endpoints even when stream works.

| Method | RestoreWeave meaning | Exit |
| --- | --- | --- |
| `search2` / `search3` | `search.query` plus in-memory filter of `audio.list` | Query by title/artist/album returns the same `SubjectRef` as `audio.list` |
| `getCoverArt` | Deterministic placeholder until a later isolated cover processor exists | Clients receive an image; no APIC parser in-process |
| `getPlaylists` / `getPlaylist` | Empty honest list | Client does not crash; creating a playlist is refused, not stored as a second library |
| `star` / `unstar` / `getStarred2` | `TAG` body `starred` | Star survives `annotation.list` and client replacement |
| `getBookmarks` / `createBookmark` / `deleteBookmark` | `PROGRESS` JSON | Position is a portable annotation |
| `getUser` / `getScanStatus` / `getOpenSubsonicExtensions` | Handshake only | Client login completes |
| `getRandomSongs` | Shuffled `audio.list` | Returns existing subjects only |
| `f=xml` browse | Same payload as JSON | XML clients can list artists/albums/songs |

Still out of D1: transcoding, multi-workspace, non-loopback bind, cover extraction, playlist collections, Jellyfin APIs.

### D2 — Client-viable OPDS

Status (2026-08-19): **adapter-harness-complete; maintenance-only**. `/opds/search`, `start`/`count` pagination with `rel=next`, acquire, and `POST /opds/progress` JSON (`subject_ref`, `position_ms`, `completed`) write `PROGRESS` with `source=opds`. Covered by `TestExperienceSurfacesOverCommandABI`; no pinned live reader is qualified.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| `/opds/search` | `books.list` filter plus `search.query` | Title/author query returns acquisition links |
| Pagination | `start` / `count` over works | Feeds stay bounded |
| Acquire | Existing `content.*` loop | Bytes match restore SHA-256 |
| Progress | RestoreWeave JSON write-back on `/opds/progress` | Do not invent a private KOReader sync protocol in D2 |

KOReader’s built-in progress sync is not OPDS. D2 ships acquire and a documented RestoreWeave progress POST. A KOReader-specific sync adapter is a later experiment, not a second catalog.

### D3 — Thin Inbox shell

Status (2026-08-19): **optional shell implemented; workflow incomplete and maintenance-only**. Embedded `inbox.html` at `/inbox` and `/inbox/api/*`. Item JSON walks `namespace.stat` for a display path and includes `snapshot_ref`, representations, and annotations. There is no Inbox drop/attach/import action; ingest still occurs through the CLI. The shell is not `RW-MVP-1` restore authority or completion evidence.

Must have: triage/status, search, item detail (provenance, annotations, representations), verify, restore.

Must not: open repository packs, open SQLite files, mutate the source tree, embed File Browser’s writable job.

Harness scope: after a tree has already been ingested through the CLI, find an item in the shell and exercise the restore operations. CLI-only recovery still works if the shell is deleted. This does not prove a one-surface import-to-restore workflow.

### D4 — Item-page preview

Status (2026-08-19): **adapter-harness-complete; maintenance-only**. Audio preview streams exact `content.*` bytes; text/EPUB shows a short extract; the page writes `PROGRESS` through the same OPDS progress command.

Harness evidence: fixture preview/read updates `PROGRESS`, and exact restore still matches. A real browser media session was deliberately not required, so this cannot be called client-experience qualification.

### D5 — Pinned request-sequence replay

Status (2026-08-19): **adapter-harness-complete; live client qualification not performed**. This host has Supersonic.app **0.22.1** (`io.github.supersonic-app.supersonic`). `TestD5PinnedSupersonicCallSequence` replays the expected XML methods. It is useful adapter regression evidence, but no GUI session, real library behavior, or pinned OPDS client has been qualified, and none is a core release gate.

Pinned OPDS client: none installed. KOReader private sync stays out.

Replay evidence (all green):

1. Browse + stream/acquire a fixture library — harness green for the pinned OpenSubsonic method sequence.
2. Star or bookmark returns on a second client or via `annotation.list` — harness green for star → `annotation.list`.
3. No second recovery authority (client DB loss does not lose RestoreWeave identity).

Keep the replay as regression coverage only. Do not turn live GUI qualification into core work and do not write a Feishin substitute. If a later adapter profile claims live-client support, it must define a separate qualification matrix rather than relabel this replay.

### D6 — File egress, not a RestoreWeave FUSE

Status (2026-08-15): **closed**. Not a RestoreWeave product job. Source `mod_time` / `uid` / `gid` already project into `NamespaceEntry` for our own records. We do not need a live `/dev/fuse` adapter, a VM, or macFUSE.

Operators who want a mount use other tools (rclone, sshfs, the NAS SMB share, WebDAV helpers) against a restored directory or against exact `FileAccess` / `content.*` bytes. RestoreWeave owns identity, catalog, annotations, verify, and restore. It does not own the kernel filesystem illusion.

The former in-tree go-fuse adapter, command ABI, CLI verbs, and dependency have been removed. Same-UID “point a media server at a mount” is not a catalog proof.

### D7 — Processors and repository

Status (2026-08-19): **candidate foundation implemented; qualification open**. Not blocked on FUSE. Raw and local-zstd drivers pass independent SHA-256 readback; local zstd also passes compression, whole-file dedup, corruption, relocation, profile-isolation, and signed-restore tests. Encryption, chunking, GC/repair, corpus measurements, mature-engine qualification, and reader closure remain. Isolated Tika/libarchive/ffprobe need Linux bubblewrap only if they join the default pack; otherwise keep them out. Cover-art extraction belongs here as an isolated processor, not as a new in-process ID3 picture parser.

### D8 — Later

Status (2026-08-14): **not started**. Jellyfin-style APIs, video transcode, game inventory, source retirement, Docker/installable release. After `RW-MVP-1` acceptance.

## 4. Mapping to existing work

| This plan | Existing document |
| --- | --- |
| Recovery proof | `RW-CORE-1` / completion plan Phase A–B |
| D3 Inbox | `RW-CATALOG-1` visible acceptance; not an MVP restore dependency |
| D1 + D4 music | Catalog slice toward `RW-AUDIO-1`, still not a player product |
| D2 books | Catalog slice toward `RW-BOOK-1`, still not a reader product |
| D6 | Closed: not a RestoreWeave FUSE job; see remaining-work-and-closed-decisions.md |
| D7 | Completion plan Phase B + adoption default pack |
| D8 | Completion plan Phase C items 4–5 |
| Discovery beyond lexical | [Index Dimension Plan](index-dimension-plan.md) I1–I9 path is in tree. Facades stay on `search.query` |

## 5. What would reverse a row

- If a pinned OpenSubsonic client cannot complete login without a method not listed in D1, add that method to D1 before starting D3.
- If cover placeholders block adoption, extract covers in D7’s isolated pack; do not grow the in-process tag parser.
- If File Browser can be constrained to the command ABI with source-tree mutation off, D3 may reuse its shell. Test the constraint.
- GitHub stars do not move a phase. A NAS-catalog check may change which client is pinned in D5.

## 6. Non-goals

The completion-plan non-goals still hold: no fake-CAS replacement in this Darwin session, no engine selection, no writable NAS, no `allow_other`, no ffmpeg/Tika as identity, no requirement-document expansion, no player or reader shipped inside `restoreweaved`.
