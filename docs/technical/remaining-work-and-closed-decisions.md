# Remaining Work and Closed Decisions

> **Status:** Informative checkout record, 2026-08-14. This document does not add product requirements and does not select a release repository engine. It exists so later work does not reopen closed product decisions or confuse a developer-laptop gap with a user job. **Occam:** do not add a subsystem when an existing surface plus a foreign tool already does the job.

Authority stays in the existing requirement set. Sequencing stays in [Implementation Completion Plan](implementation-completion-plan.md) and [Experience Completion Plan](experience-completion-plan.md).

## 0. What the product is

RestoreWeave is the **content and recovery plane**. It is not a NAS OS, not a media server, and not a client suite.

**Must have** (without these it is not RestoreWeave):

- Exact identity of bytes (`ContentIdentity` / SHA-256) and a stable `SubjectRef`.
- Ingest a tree without racing writers into a lie.
- An immutable snapshot + original-path namespace.
- Independent verify and restore that match SHA-256.
- A catalog that can find a subject by path, type, tag, note, or extracted text.
- Durable annotations (`TAG` / `NOTE` / `PROGRESS`) that survive index rebuild and client loss.
- Bounded exact reads (`content.*` / `FileAccess`).
- Processors that may fail; they must not block exact ingest / verify / restore.

**Must keep** (delete the UI, the facade, the FTS file, even the SQLite catalog, and these still mean the same thing):

- The exact payload in the repository.
- The portable snapshot / publication.
- Subject identity and annotation export.
- The rule that a player library or search index is never recovery authority.

**May have** (replaceable, optional, later — not the definition):

- A real repository engine (after readback gates). Isolated heavy parsers. Inbox page. OpenSubsonic / OPDS HTTP APIs. CLI / MCP. Later video/books/games *catalog slices*. Foreign tools mounting a restored directory.

**Must not become:** writable NAS, embedded player/reader/media server, our own FUSE/SMB/WebDAV product, a second catalog inside a client, Tika/ffmpeg as identity.

## 1. What RestoreWeave owns

Identity, admission, snapshots, catalog, annotations (`TAG` / `NOTE` / `PROGRESS`), the daemon command envelope, Inbox JSON, narrow OpenSubsonic / OPDS HTTP APIs, `FileAccess` / `content.*`, `snapshot.verify`, and `plan.restore`.

Those surfaces hand out **exact bytes and stable IDs**. Other programs may play, read, share, or mount from there.

**ABI vs API.** The Unix-socket command envelope is the daemon’s internal contract (`rw` and the facades call it). Clients do not speak that socket. Clients speak HTTP APIs: OpenSubsonic `/rest/*`, OPDS `/opds`, Inbox `/inbox/api/*`. Do not make operators or app authors drop to the socket. Do not add a second public REST catalog beside those three.

## 2. Closed decisions — do not reopen

| Decision | Meaning | Common mistake |
| --- | --- | --- |
| **Occam's razor** | Do not add a mount server, WebDAV, SMB, player, or second catalog. `gateway.mount` is `unimplemented` and names `plan.restore`. | “We should make it mountable ourselves” |
| **No RestoreWeave FUSE product** | We do not ship, qualify, or block on a kernel FUSE server. The in-tree go-fuse adapter is leftover. `rw mount` refuses. | Treat `/dev/fuse`, macFUSE, a VM, or `allow_other` as remaining work |
| **Foreign tools mount** | Operators who want a folder use rclone, sshfs, NAS SMB, WebDAV helpers, or similar against a **restored directory** or exact read bytes | Build WebDAV/SMB/FUSE inside `restoreweaved` so “it can mount” |
| **Two proofs stay separate** | Recovery = ingest → verify → restore SHA-256. Product = one Inbox search, one open, one restore | “A client can play” or “Navidrome scanned a folder” as either proof |
| **Adapters, not products** | Existing clients attach to HTTP APIs. We adapt OpenSubsonic/OPDS to our daemon. We do not vendor those apps. | Fork Feishin/Navidrome/KOReader; invent a RestoreWeave player; publish the Unix socket as the app SDK |
| **Constrained clients** | Only `rw`, read-only MCP, one Inbox page, plus foreign OpenSubsonic/OPDS apps. No native app, no full WebUI, no second catalog API. | A RestoreWeave iOS/Android/Vue suite |
| **Fake CAS is not the release engine** | In-tree driver for tests. Restic/Kopia CLI green ≠ selected engine | Replace it because a blog or star count prefers one name |
| **Darwin ≠ missing POSIX** | This Mac can run catalog, CLI, sockets, Inbox. It cannot close Linux namespace gates | Use `sandbox-exec` as bubblewrap; call Darwin “not Unix” |
| **Default ingest stays in-process** | Processor failure must not block exact ingest / verify / restore | Make Tika/ffmpeg/ffprobe core identity |
| **Do not expand requirement documents** | Record implementation status here and in the completion plans | Add FUSE, WebUI, or players to `RW-MVP-1` because they feel missing |

## 3. What this checkout already has

- Exact ingest / verify / restore over the fake CAS, with SHA-256 tests.
- Command ABI, CLI, read-only MCP.
- Loopback facade: OpenSubsonic (client-viable methods, CORS, `enc:` / salt-token auth, honest empty handshake methods), OPDS (search, pagination, acquire, JSON progress), Inbox (`/inbox`).
- In-process EXTRACT for UTF-8 text, ID3/FLAC/OGG tags, EPUB OPF.
- Portable source stat (mtime/uid/gid) on namespace records.
- `recovery.export` copies the existing portable snapshot JSON to a new file and refuses overwrite. No credentials.
- `snapshot.diff` compares two repository manifests by original path (added / removed / moved / content / metadata / type). Catalog-free.
- `namespace.resolve` walks display-name components to a catalog entry id and does not follow symbolic links.
- `representation.list` reports catalog representations for one subject or file version. It does not open content. Placement is `unknown` without the exact lane, and SHA-256 verified when the fake CAS is present.

`GOMODCACHE=… GOPROXY=off go test ./...` green on this Darwin host is **not** `RW-MVP-1` acceptance and **not** an engine selection.

## 4. What is actually left

Ordered by whether it is our job.

### Next, still our surface

| Item | Why it is still open | Done when |
| --- | --- | --- |
| **D5 live clients** | Harness covers the facade. No new client or protocol. Point an already-installed OpenSubsonic or OPDS app at the existing loopback facade | One existing client browses, fetches exact bytes, and writes star/progress that `annotation.list` can see. Do not write a Feishin substitute |
| **Honest repository** | Fake CAS cannot be the release store | Independent SHA-256 readback, then NAS/S3 as needed. Only then replace the fake driver. Still no engine name in portable records |
| **`RW-MVP-1` acceptance** | Requirements exist; the release tuple does not. `recovery.export` / `snapshot.diff` / `namespace.resolve` / `representation.list` now exist on the fake CAS; plan/apply/doctor and a release engine still do not | The existing MVP contract, not a new FUSE or WebUI gate |

### Later, only if we take on heavier parsing

| Item | Why it can wait | Done when |
| --- | --- | --- |
| Isolated Tika / libarchive / ffprobe | Current in-process extracts already cover the catalog slice. Isolation is so a hostile file cannot own the host | Linux bubblewrap (or keep those processors out of the default pack). NOTICE/SBOM. LGPL-only FFmpeg if ffprobe ships. Cover-art extraction lives here, not as an in-process APIC parser |

### Not our job / not this slice

| Item | Disposition |
| --- | --- |
| Live FUSE / macFUSE / VM-for-FUSE / `allow_other` | Closed. Foreign mount tools only |
| KOReader private sync protocol | Not OPDS. Optional D5 experiment, not a second catalog |
| Player / reader / Jellyfin / video transcode | After `RW-MVP-1`. Never inside `restoreweaved` |
| Writable NAS, source retirement, Docker/installable release | Later named profiles |
| Deleting the leftover go-fuse package | Optional cleanup. Do not “finish” it instead of deleting or ignoring it |

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

## 7. D5 with what already exists

No new binary. On a machine that already has a Subsonic or OPDS client:

```text
restoreweaved --socket … --catalog … --repository … \
  --facade-listen 127.0.0.1:4534 --facade-token <token> --facade-workspace <id>
```

Point Feishin, DSub, or another OpenSubsonic client at `http://127.0.0.1:4534` (password = token; `enc:` and `t`/`s` auth also work). Point an OPDS client at `/opds`. Confirm `annotation.list` after a star or progress write. Inbox is `http://127.0.0.1:4534/inbox?token=<token>`.

Want a folder? `rw restore <snapshot-ref> <empty-dir>`, then the operator’s SMB/rclone/sshfs. Not `rw mount`.
