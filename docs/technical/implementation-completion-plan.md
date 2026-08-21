# Implementation Completion Plan

> **Superseded for core ordering:** The single dependency-ordered path is now [Core MVP Execution Plan](core-mvp-execution-plan.md). This document remains a host-specific record for Darwin/Linux and repository qualification observations; it must not be used to reorder the content-core phases.

This plan is the remaining coding path for the frozen content/recovery slice. It does not add product requirements or select a release repository engine. Darwin is Unix and can run CLI probes. RestoreWeave does not own a mount service; see [Remaining Work and Closed Decisions](remaining-work-and-closed-decisions.md). Isolated processors may later need Linux bubblewrap.

The only current product order is [Core MVP Execution Plan](core-mvp-execution-plan.md): configuration and protection -> plan/apply -> recovery closure -> descriptions/default discovery plus qualified storage -> views/exports -> release. Inbox, OpenSubsonic, OPDS, music, and reader slices below are historical adapter exercises and are maintenance-only; they cannot move ahead of or add completion credit to that order.

## Honest host split

| Lane | What “done” means | This Darwin checkout |
| --- | --- | --- |
| Darwin catalog + control plane | OGG tags in `audio.list`; `books.list` for TXT/Markdown and EPUB OPF; CLI/MCP symmetry; tests green | Runnable here |
| Darwin / Unix CLI | restic/kopia filesystem probes, Unix sockets, SCM_RIGHTS, grpc-go wrapping | restic and kopia CLI probes green on this host; not an engine selection |
| Linux kernel ABI | bubblewrap only if isolated heavy parsers join the default pack | Parser isolation only; no mount gate |
| Object storage / release engine | Embedded local-zstd candidate exists; mature release engine still needs NAS/S3, failure, GC, migration, and reader gates | Candidate runnable here; release qualification remains |
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

Status (2026-08-15): **Darwin-closable half is done.** NAS/S3, engine replacement, and Linux bubblewrap remain later. This is not `RW-MVP-1` and not an engine selection.

Unix CLI probes (restic and kopia) run on Darwin. This checkout measured restic on PATH and kopia via `KOPIA_BIN`; passing does not select a release engine. grpc-go wrapping of the private RUN_STAGE messages is tested here (`TestGRPCRunStagePassesBytesOnFDsNotInMessages`). FDs still travel on SCM_RIGHTS; default ingest remains in-process.

Closed on this Darwin checkout:

1. Independent SHA-256 readback at the blob driver (`repository/qualify.DriverGates`) and at the snapshot (`snapshot.verify`). Default verify is `full-bytes`. Declared modes are `authenticated-metadata`, `sampled-content`, `full-bytes`, `restore-drill` (destination required; only this mode may set `restore_verified`), and `clean-recovery` (catalog-free full-bytes). Sampled work is never relabeled as full-bytes.
2. `annotation.import` takes an explicit conflict policy: `fail` (default), `keep-local`, or `keep-imported`. `keep-imported` revises a live local row; it does not rewrite a tombstone or transplant a foreign revision number.
3. RestoreWeave does not ship or qualify a mount server. File-shaped access is `plan.restore`, export materialization, and `FileAccess`. Foreign tools may consume those outputs.

Still later, not this Darwin session:

1. Isolated processors may later need bubblewrap (Linux namespaces, not POSIX). Default ingest stays in-process. This is a parser-safety gate, not a mount gate.
2. NAS/S3 performance against a real remote target.
3. Qualifying a release repository after those Driver gates. The local-zstd measurement candidate and research preference do not select the engine.

## Phase C — later experiences and release

Introduce experience UIs as separate clients over RestoreWeave-owned surfaces, not as code inside `restoreweaved`. The long sequence is [Experience Completion Plan](experience-completion-plan.md). Named discovery dimensions are [Index Dimension Plan](index-dimension-plan.md). See [Whole-Architecture Open-Source Reference](architecture-open-source-reference.md). Product proof and engineering gates stay on separate tracks.

1. Keep the command ABI as the only catalog/read/annotation path. A thin Inbox shell now binds that ABI at `/inbox` when the loopback facade is enabled; it is not an `RW-MVP-1` restore dependency.
2. Protocol facades first: loopback OpenSubsonic for Subsonic clients and OPDS for KOReader-class clients. Song IDs are `SubjectRef`. `scrobble` writes a `PROGRESS` annotation. Do not embed a player or media server.
3. Do not grow a RestoreWeave mount daemon. If someone wants a folder, they restore or export it and use their own tool.
4. Video browse/subtitles after isolated ffprobe. Application/game inventory stays read-only.
5. Source retirement, Docker, and installable release after `RW-MVP-1` acceptance.

## Non-goals that stay non-goals

- Promoting or selecting a release repository in this host-specific plan
- Claiming Kopia or Restic is the release engine
- Writable NAS, `allow_other`, or weakening `ro,nodev,nosuid,noexec`
- ffmpeg/Tika as core identity
- Expanding requirement documents
- Shipping a player or reader UI in this slice
