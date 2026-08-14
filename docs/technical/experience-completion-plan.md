# Experience Completion Plan

> **Status:** Informative implementation plan, recorded 2026-08-14. This document does not add product requirements, select a release repository engine, or make a WebUI / player / reader part of `RW-MVP-1`. Authority stays in the existing requirement set. Open-source introduction order stays in [Whole-Architecture Open-Source Reference](architecture-open-source-reference.md). Darwin/Linux engineering gates stay in [Implementation Completion Plan](implementation-completion-plan.md).

The job is to finish the **experience introduction path** already decided: connect existing open-source clients through RestoreWeave-owned surfaces. Do not embed a media server. Do not wait for live FUSE to prove the catalog.

## 1. What “complete” means

Two proofs stay separate.

| Proof | Complete when | Not complete when |
| --- | --- | --- |
| **Recovery** | CLI/MCP ingest → verify → restore matches SHA-256 without a UI | A client can play a file |
| **Product** | One Inbox search, one authorized open, one restore; specialized clients can attach without creating a second recovery authority | Three foreign UIs are still the weekly entry point |
| **Interop** | A current OpenSubsonic client and a current OPDS client can browse, fetch exact bytes, and write progress/stars back to `SubjectRef` | Navidrome can scan a FUSE mount |

A phase is done only when the listed exit test is green. “Looks like Subsonic” is not an exit.

## 2. Current checkout (2026-08-14, updated same day)

Present:

- Command ABI: ingest, search, tags/notes, `PROGRESS`, `audio.list`, `books.list`, `content.open`/`read`/`close`, verify, restore.
- Optional loopback facade (`restoreweaved --facade-listen`): OpenSubsonic client-viable methods; OPDS navigation/search/pagination/acquire; `POST /opds/progress` JSON write-back; thin Inbox at `/inbox`.
- IDs on the facade are `SubjectRef`. The facade calls the command ABI only.
- Inbox item page can stream exact bytes and write `PROGRESS`. Verify/restore stay command-ABI calls.
- Namespace records carry source mtime/uid/gid. RestoreWeave does not own a FUSE server; foreign tools may mount a restored tree.
- Fake directory CAS is still the in-tree driver.

Absent or later:

- Live client qualification (Feishin, DSub, KOReader, or equivalents) has not been run on this host.
- KOReader’s private progress-sync protocol is not implemented; D2 ships RestoreWeave JSON progress, not that protocol.
- Isolated Tika/ffprobe, NAS/S3, engine selection, video/release packaging. Live FUSE is a closed non-goal, not an absence.

## 3. Long sequence

Engineering gates (Phase B) and this experience sequence run in parallel. Experience work must not wait for `/dev/fuse`. Experience work must not replace the fake CAS.

```text
D1  Client-viable OpenSubsonic
D2  Client-viable OPDS
D3  Thin Inbox shell over the command ABI
D4  Item-page FileAccess preview + PROGRESS
D5  Live client qualification
D6  File egress via restore / FileAccess (not our FUSE)
D7  Isolated processors and remaining repository gates
D8  Later video / release packaging
```

### D1 — Client-viable OpenSubsonic

Status (2026-08-14): implemented in `server/internal/gateway/protocol` and covered by `TestProtocolFacadeUsesCommandABI` plus `TestExperienceSurfacesOverCommandABI`. Live Feishin/DSub login is still D5.

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

Status (2026-08-14): implemented. `/opds/search`, `start`/`count` pagination with `rel=next`, acquire, and `POST /opds/progress` JSON (`subject_ref`, `position_ms`, `completed`) write `PROGRESS` with `source=opds`. Covered by `TestExperienceSurfacesOverCommandABI`.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| `/opds/search` | `books.list` filter plus `search.query` | Title/author query returns acquisition links |
| Pagination | `start` / `count` over works | Feeds stay bounded |
| Acquire | Existing `content.*` loop | Bytes match restore SHA-256 |
| Progress | RestoreWeave JSON write-back on `/opds/progress` | Do not invent a private KOReader sync protocol in D2 |

KOReader’s built-in progress sync is not OPDS. D2 ships acquire and a documented RestoreWeave progress POST. A KOReader-specific sync adapter is a D5 experiment, not a second catalog.

### D3 — Thin Inbox shell

Status (2026-08-14): implemented as embedded `inbox.html` at `/inbox` and `/inbox/api/*`. Not `RW-MVP-1` restore authority.

Must have: triage/status, search, item detail (provenance, annotations, representations), verify, restore.

Must not: open repository packs, open SQLite files, mutate the source tree, embed File Browser’s writable job.

Exit: drop a mixed tree → find any item in the shell → restore original bytes. CLI-only recovery still works if the shell is deleted. Harness: `TestExperienceSurfacesOverCommandABI`.

### D4 — Item-page preview

Status (2026-08-14): implemented. Audio preview streams exact `content.*` bytes; text/EPUB shows a short extract; the page writes `PROGRESS` through the same OPDS progress command.

Exit: play/read one admitted subject; `annotation.list` shows `PROGRESS`; exact restore still matches. Harness green. Browser `FileAccess` and a real `<audio>` session are not required for this exit.

### D5 — Live client qualification

Status (2026-08-14): **harness only**. OpenSubsonic handshake/search/star/bookmark/stream and OPDS search/pagination/progress are covered in-process. Feishin, DSub, KOReader, or equivalents have not been run.

Pin one OpenSubsonic client and one OPDS client. Record version, the method they call, and failures.

Exit:

1. Browse + stream/acquire a fixture library.
2. Star or bookmark returns on a second client or via `annotation.list`.
3. No second recovery authority (client DB loss does not lose RestoreWeave identity).

Unverified until someone actually runs those clients. Absence of a NAS-forum survey remains a coverage gap.

### D6 — File egress, not a RestoreWeave FUSE

Status (2026-08-14, revised same day): **not a RestoreWeave product job.** Source `mod_time` / `uid` / `gid` already project into `NamespaceEntry` for our own records. We do not need a live `/dev/fuse` adapter, a VM, or macFUSE.

Operators who want a mount use other tools (rclone, sshfs, the NAS SMB share, WebDAV helpers) against a restored directory or against exact `FileAccess` / `content.*` bytes. RestoreWeave owns identity, catalog, annotations, verify, and restore. It does not own the kernel filesystem illusion.

The in-tree go-fuse adapter may remain unused. Do not spend further work qualifying it. Same-UID “point a media server at a mount” is not a catalog proof.

### D7 — Processors and repository

Status (2026-08-14): **not started**. Not blocked on FUSE. Isolated Tika/libarchive/ffprobe need Linux bubblewrap only if they join the default pack; otherwise keep them out. Remaining Driver gates (independent SHA-256 readback, then NAS/S3) come before fake-CAS replacement. Cover-art extraction belongs here as an isolated processor, not as a new in-process ID3 picture parser. This Darwin session does not replace the fake CAS.

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

## 5. What would reverse a row

- If a pinned OpenSubsonic client cannot complete login without a method not listed in D1, add that method to D1 before starting D3.
- If cover placeholders block adoption, extract covers in D7’s isolated pack; do not grow the in-process tag parser.
- If File Browser can be constrained to the command ABI with source-tree mutation off, D3 may reuse its shell. Test the constraint.
- GitHub stars do not move a phase. A NAS-catalog check may change which client is pinned in D5.

## 6. Non-goals

The completion-plan non-goals still hold: no fake-CAS replacement in this Darwin session, no engine selection, no writable NAS, no `allow_other`, no ffmpeg/Tika as identity, no requirement-document expansion, no player or reader shipped inside `restoreweaved`.
