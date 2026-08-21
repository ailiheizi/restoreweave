# Tool-Core Wedge Plan

> **Status:** Informative plan, 2026-08-16. This does not add product requirements, does not expand `RW-MVP-1`, and does **not** implement any new adapter. It records a wedge: start as a text-shaped tool, then sit under novels / memories / quotes, then under video files. Authority stays in [Ecosystem and Vertical Applications](../requirements/ecosystem-and-vertical-apps.md). Jobs stay in [Foreign App Jobs](foreign-app-jobs.md). Closed decisions stay in [Remaining Work and Closed Decisions](remaining-work-and-closed-decisions.md).

**Answer:** RestoreWeave should stay a **small tool** that other programs call. The first useful wedge is exact text. Novels, 语录, and 回忆 are the same tool with more files. Video is still the same tool — a file plus `PROGRESS` — until a later isolated processor is registered. Do not fill those sockets now.

## 0. Seed fingerprint

| Piece | Meaning |
| --- | --- |
| Job | Keep bytes, find them later, prove them, hand them to a foreign app |
| Mechanism | Content identity + original-path snapshot + disposable indexes + durable annotations |
| Loop | ingest → search/tag → open or restore → foreign app does play/read/edit |
| Assumption | Operators will call a CLI / MCP / old HTTP standard instead of a native suite |
| Failure if | The daemon grows a reader, player, quote editor, or fourth REST catalog |

Siftline run `rw-wedge-2026-08-16` (GitHub + HN + Tavily). First “content-addressed archive library” query returned IPLD/CASC noise — vocabulary failure, not empty demand. Pivot to operator words (`git-annex`, `casync`, “sync is not backup”, note-export) produced the sources below.

## 1. Research frontier

| Branch | Relation | Platform | Query that mattered |
| --- | --- | --- | --- |
| Embeddable CAS as a *tool* | mechanism | GitHub | `git-annex`, `casync` |
| Files as the attach surface | combination | HN | item [47286408](https://news.ycombinator.com/item?id=47286408) |
| Note/quote export pain | demand | Tavily | archive exports without another note app |
| Gallery is not recovery | counterexample | HN + Immich docs | `immich is not a backup` |

## 2. Findings that change the shape

| Source | Relation | Why it matters |
| --- | --- | --- |
| [git-annex](http://git-annex.branchable.com/) + [rclone remote](https://github.com/git-annex-remote-rclone/git-annex-remote-rclone) | mechanism | Identity + special remotes. Not a reader. People already attach storage tools to it. |
| [casync](https://github.com/systemd/casync) / [desync](https://github.com/folbricht/desync) | mechanism | Content-addressed *sync tool*. No media UI. The product is the CLI. |
| [DataLad](https://github.com/datalad/datalad) | descendant | Wraps git-annex for datasets. Still a tool, not a novel reader. |
| [Files are the interface](https://news.ycombinator.com/item?id=47286408) (296 points, 2026-03-07) | combination | Humans and agents already meet at files. A second app DB is the thing to avoid. |
| [12 000 iPhone notes](https://www.reddit.com/r/applehelp/comments/mc2vqp/i_have_12000_iphone_notes_how_do_i_export_them) | demand | Quotes, thoughts, stories stuck in a capture app. The costly job is *export and keep*, not another editor. |
| [Archive Apple Notes to stay lean](https://talk.macpowerusers.com/t/do-your-export-archive-apple-notes-to-keep-it-lean-and-mean/37630) | demand | Capture app stays small; archive is a separate tree. |
| [HN: backup Immich or lose the library](https://news.ycombinator.com/item?id=47492999) and [Immich backup doc](https://github.com/immich-app/immich/blob/master/docs/docs/administration/backup-and-restore.md) | counterexample | A polished domain app is not recovery authority. Same trap as becoming Jellyfin for 追番. |

**Stars are not demand.** casync ~1.6k and DataLad ~0.6k only show the *tool* shape is a known category. The 12 000-notes thread and Immich “you still need a backup” are the costly signals.

**Strongest counter-story:** operators only adopt a native suite. That is a failed attach, not a reason to grow the core. **Smallest reverse:** they refuse to ingest unless we also edit/play inside `restoreweaved` — that would be a different product.

## 3. Core that stays a tool

This is the whole product for the wedge. Everything else is a socket.

```text
plan.ingest          admit a settled tree
SubjectRef + SHA-256 identity
namespace            original paths
search.query         lexical default; other dimensions may be UNAVAILABLE
annotation           TAG / NOTE / PROGRESS
content.open/read    bounded exact bytes
snapshot.verify
plan.restore         empty dir or preflight
capability.list      honest sockets
```

Public attach (already exist; do not add a fourth):

```text
rw / read-only MCP     tool
Inbox JSON             mixed find / restore
OPDS                   books when a reader attaches
OpenSubsonic           music when a player attaches
restored directory     everything else
```

Unix-socket command ABI stays internal. App authors do not speak it.

**Must stay out of the core:** reader, player, quote editor, TMDB scrape, WhatsApp pack export, FUSE/SMB, video transcode, a `quote` subject type.

## 4. Sockets we keep — adapters we do not fill

The host already has replaceable seams. **Leave them empty** until a later isolated pack is actually taken.

| Socket | Already in tree | Do not implement now |
| --- | --- | --- |
| `Processor` (`CapabilityID` + `RUN_STAGE`) | `extract.text.v1`, `extract.audio.tags.v1`, `extract.book.meta.v1` | `extract.video.*`, EXIF, OCR, Chromaprint, CLIP weights |
| Opt-in processors | fixture fingerprint / embed (not in `DefaultProcessors()`) | Registering them by default |
| `IndexProvider` / `QueryProvider` | five named dimensions; graph is a catalog projection | A quote index, a TV library index |
| `RepositoryDriver` | raw development CAS plus local-zstd candidate | Selecting or promoting a release engine in this plan |
| HTTP facades | OpenSubsonic, OPDS, Inbox | Jellyfin API, sticker API, Flomo API |
| `RetrieverDriver` | reserved in requirements | Sonarr / magnet |

Unknown `capability_id` already fails. Missing later dimensions already degrade. That *is* the adapter contract. Do not invent stub IDs that pretend a video pack exists.

## 5. Wedge — text first, then the same files grow

Each step reuses the core. No new `rw` subcommand. No new public REST.

### T0 — Minimal text (do this shape, no new code required)

A folder of UTF-8 `.txt` / `.md` is enough.

```text
inbox/
  2026-08-16-quote.md      "那句只有自己记得的话"
  2026-08-16-memory.md     a short 回忆
```

```text
rw ingest inbox/                         # creates a READY plan; review it
rw plan apply <ingest-plan-id> \
  --workspace <workspace-id> --digest <plan-digest>
rw search "那句只有自己记得的话"
rw annotation tag <subject> memory
rw restore <snapshot> /tmp/out            # creates a read-only restore plan
rw plan apply <restore-plan-id> \
  --workspace <workspace-id> --digest <plan-digest>
```

Immediate value: exact keep, lexical find, tag, prove, restore. Foreign capture (Flomo, Notes, share sheet) stays foreign. We ingest the **export**.

### T1 — Novels (same tool + existing OPDS)

Add EPUB / more `.txt` to the same tree. `extract.book.meta.v1` already runs on EPUB. KOReader speaks OPDS. Do not ship a reader. Do not take KOReader private sync.

```text
novels/foo.epub
quotes/bar.md
```

One Inbox search hits both. OPDS acquire is the adapter. Core does not grow.

### T2 — 语录 / 回忆 / clippings (still text)

Kindle `My Clippings.txt`, Flomo HTML export, Telegram JSON export, a markdown vault *copy* (sync ≠ backup). Same T0 operations. `NOTE` holds the human sentence if the file is a dump. Do not become Obsidian or Flomo.

### T3 — Video as a file (still no video adapter)

```text
shows/Nightfall/S03E07.mkv
shows/Nightfall/S03E07.zh.srt
```

Ingest as generic subjects. `PROGRESS` on the mkv `SubjectRef`. Play: restore or `content.*`, then IINA / mpv / Infuse. Sidecar `.srt` is another exact file in the same snapshot. Do **not** add `video.list`, ffprobe, or a Jellyfin facade in this wedge.

### T4 — Later, only if we take the pack

Then — and only then — register an isolated `extract.video.*` or EXIF processor. `capability.list` flips that row from absent/UNAVAILABLE to AVAILABLE. Facades stay old standards. The core command set does not change.

```text
T0 text files
  -> T1 novels via OPDS
  -> T2 quote / memory / chat exports
  -> T3 video files + PROGRESS
  -> T4 optional isolated processors
```

Images / stickers follow T3: files in the Inbox, restore into Immich / a pack tool. Same socket, later pack.

## 6. Worked examples (attach, do not adapt)

**Quote vault.** Operator exports Flomo or dumps Notes to `quotes/*.md`. `plan.ingest`. Search a half-remembered sentence. Tag `语录`. Years later `plan.restore`. Flomo is still the capture UI.

**Novel + quote together.** Same workspace. KOReader opens the EPUB over OPDS. Inbox finds the quote file. One `SubjectRef` space.

**回忆 that is a voice memo plus a paragraph.** `.m4a` + `.md` in one folder. Text is searchable today. Audio plays through OpenSubsonic or a restored file. No new domain type.

**美剧 remux.** Ingest the mkv. Write `PROGRESS`. Sonarr replaces the file next month; that is a new ingest / `snapshot.diff`. Watch state stays on our subject if the operator tagged it, not on Jellyfin’s GUID. Player stays foreign.

**Agent.** Read-only MCP already exposes search and bounded `content.*`. An external agent classifies or proposes tags. It does not become a second catalog.

## 7. What this checkout does *not* start

- No `extract.video`, EXIF, OCR, or quote schema.
- No `rw quote` / `rw watch` / `rw sticker`.
- No fourth HTTP catalog.
- No filling OpenSubsonic `getVideos` with a fake library.
- No release repository selection or promotion in this plan.

The honest remaining core work still includes a qualified repository; the local-zstd candidate is only measurement infrastructure. This wedge does not wait on that work, and it does not pretend it is done.

## 8. Next three checks (only if the wedge is doubted)

1. Ingest a real Flomo/Notes/Telegram export tree on this checkout; confirm lexical hit + restore SHA-256. No new processor.
2. Point KOReader at loopback OPDS for one EPUB that sits next to a quote `.md`. Replay HTTP, do not click a GUI as the proof.
3. Ingest one mkv + srt; write `PROGRESS`; delete nothing in the player. If that is unusable without posters, the attach failed — still do not become Jellyfin.
