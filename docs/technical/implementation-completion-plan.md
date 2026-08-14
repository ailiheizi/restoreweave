# Implementation Completion Plan

This plan is the remaining coding path for the frozen NAS vertical slice. It does not add product requirements. It does not select a release repository engine. Darwin is Unix and can run CLI probes. RestoreWeave does not own a FUSE server; see [Remaining Work and Closed Decisions](remaining-work-and-closed-decisions.md). Isolated processors may later need Linux bubblewrap. That is not a mount gate.

Canonical product order stays: exact core (`RW-CORE-1`) → Inbox/catalog (`RW-CATALOG-1`) → music/reader catalog slices (`RW-AUDIO-1`, `RW-BOOK-1`) → later video, games, retrieve. Music and books here are catalogs over admitted artifacts, not a player or a reader UI.

## Honest host split

| Lane | What “done” means | This Darwin checkout |
| --- | --- | --- |
| Darwin catalog + control plane | OGG tags in `audio.list`; `books.list` for TXT/Markdown and EPUB OPF; CLI/MCP symmetry; tests green | Runnable here |
| Darwin / Unix CLI | restic/kopia filesystem probes, Unix sockets, SCM_RIGHTS, grpc-go wrapping | restic and kopia CLI probes green on this host; not an engine selection |
| Linux kernel ABI | bubblewrap only if isolated heavy parsers join the default pack | Not a FUSE gate. Darwin is Unix; namespaces are not POSIX |
| Object storage / release engine | NAS/S3 performance, then replace fake CAS | Needs credentials and remaining Driver gates |
| Later profiles | Player, reader UI, video, games inventory, source retirement, Docker/release | Out of this slice |

## Phase A — Darwin catalog plane (this host)

Status (2026-08-13): done and tested on this checkout.

1. Admit OGG Vorbis comments through `extract.audio.tags.v1` and list them with ID3/FLAC in `audio.list`.
2. Add `extract.book.meta.v1` for EPUB OPF (`dc:title` / `dc:creator` / date) using `archive/zip` and `encoding/xml`. Do not add Tika.
3. Add `.epub` and `.md` suffix rules so those files get a processing route. `.txt` already routes to UTF-8 EXTRACT; `books.list` also projects those admitted text artifacts as works.
4. Expose `books.list` on the Unix-socket control plane, `rw books list`, and read-only MCP. Keep mutation off MCP.
5. Keep default ingest `RUN_STAGE` in-process. Processor panic/timeout must still not block exact ingest, verify, or restore.

Exit: ingest a tree with MP3, OGG, TXT, Markdown, and EPUB; `audio.list` and `books.list` show titles; lexical search hits extracted titles; exact restore still matches SHA-256. Covered by processor extract tests plus `TestBooksListAfterIngest` / MCP list tests.

## Phase B — remaining qualification

Unix CLI probes (restic and kopia) run on Darwin. This checkout measured restic on PATH and kopia via `KOPIA_BIN`; passing does not select a release engine. grpc-go wrapping of the private RUN_STAGE messages is tested here (`TestGRPCRunStagePassesBytesOnFDsNotInMessages`). FDs still travel on SCM_RIGHTS; default ingest remains in-process.

Linux kernel ABI still cannot be closed on this Mac, and most of it is no longer on the product path:

1. Isolated processors may later need bubblewrap (Linux namespaces, not POSIX). Default ingest stays in-process. This is a parser-safety gate, not a mount gate.
2. RestoreWeave does not ship or qualify a FUSE server. File-shaped access is `plan.restore` and `FileAccess`. Foreign tools may mount those. The in-tree go-fuse adapter is leftover.
3. Add NAS/S3 performance and independent SHA-256 readback before replacing the fake CAS.

Only after those Driver gates pass may the fake CAS be replaced. Research preference does not select the engine.

## Phase C — later experiences and release

Introduce experience UIs as separate clients over RestoreWeave-owned surfaces, not as code inside `restoreweaved`. The long sequence is [Experience Completion Plan](experience-completion-plan.md). See [Whole-Architecture Open-Source Reference](architecture-open-source-reference.md). Product proof and engineering gates stay on separate tracks.

1. Keep the command ABI as the only catalog/read/annotation path. A thin Inbox shell now binds that ABI at `/inbox` when the loopback facade is enabled; it is not an `RW-MVP-1` restore dependency.
2. Protocol facades first: loopback OpenSubsonic for Subsonic clients and OPDS for KOReader-class clients. Song IDs are `SubjectRef`. `scrobble` writes a `PROGRESS` annotation. Do not embed a player or media server.
3. Do not grow a RestoreWeave FUSE daemon. If someone wants a folder, they restore (or read exact bytes) and mount with their own tool.
4. Video browse/subtitles after isolated ffprobe. Application/game inventory stays read-only.
5. Source retirement, Docker, and installable release after `RW-MVP-1` acceptance.

## Non-goals that stay non-goals

- Replacing the fake CAS in this Darwin session
- Claiming Kopia or Restic is the release engine
- Writable NAS, `allow_other`, or weakening `ro,nodev,nosuid,noexec`
- ffmpeg/Tika as core identity
- Expanding requirement documents
- Shipping a player or reader UI in this slice
