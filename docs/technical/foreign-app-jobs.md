# Foreign App Jobs on the Content Plane

> **Status:** Informative enumeration, 2026-08-16. This is not a requirement document and does not expand `RW-MVP-1`. It does not authorize a sticker manager, anime downloader, reader, note app, or media server inside `restoreweaved`. Authority stays in [Ecosystem and Vertical Applications](../requirements/ecosystem-and-vertical-apps.md). Closed product decisions stay in [Remaining Work and Closed Decisions](remaining-work-and-closed-decisions.md).

The question was: a cross-platform sticker manager, 追番 / 美剧 libraries, novels, and quote / memory vaults all look like separate products. Which of those jobs can sit on RestoreWeave, and which would make us become the app?

**Answer:** almost every “keep my stuff and find it later” job can sit on one mixed Inbox. The plane already promised that. The apps the user saw are **foreign clients or later domain lenses**, not new RestoreWeave products.

## 0. How to read the grades

RestoreWeave owns exact bytes (`ContentIdentity` / SHA-256), `SubjectRef`, snapshots, catalog, `TAG` / `NOTE` / `PROGRESS`, disposable search generations, bounded `content.*` reads, verify, and restore. Public attach points today: OpenSubsonic `/rest/*`, OPDS `/opds`, Inbox `/inbox/api/*`. After restore, a folder is just a folder.

| Grade | Meaning |
| --- | --- |
| **A** | The painful job is keep / find / annotate / prove / restore. The plane is the product. |
| **B** | Same job, but a foreign app still owns play / read / pack-export. Restore or FileAccess, then that app. |
| **C** | Tempting, but we would have to become the app (player, downloader, editor, chat importer). Do not. |
| **D** | Poor fit: live social, rights, device camera roll, or a second writable catalog. |

A and B are “empower.” C and D are “do not own.”

## 1. Already named (do not re-open as new products)

[Ecosystem §2.1](../requirements/ecosystem-and-vertical-apps.md) already names the jobs, not the storefront apps:

| Named job | Typical foreign app | Grade |
| --- | --- | --- |
| Drop mixed material into one Inbox | Inbox page, `rw` | A |
| Import a NAS tree without racing writers | Capture + snapshot | A |
| Find an arbitrary item later | Inbox search, MCP | A |
| Open / export / restore with proof | Inbox, `plan.restore` | A |
| Listen to a personal music library | Supersonic / Feishin / DSub on OpenSubsonic | B (`RW-AUDIO-1`) |
| Read novels, books, or comics | KOReader / any OPDS client | B (`RW-BOOK-1`) |
| Watch a video library | Infuse / IINA / mpv / later Jellyfin-class | B (`RW-VIDEO-1`) |
| Review photos and documents together | Immich / PhotoPrism after restore, or Inbox | B (`RW-IMAGE-1`) |
| Locate apps and games without launching them | Inventory lens | A then later `RW-COLLECTION-1` |
| Organize without moving cards | Tags, notes, virtual collections | A |
| Ask an external AI to classify | Read-only MCP | B |
| Reacquire a missing source | Later `RW-RETRIEVE-1` | D until that profile |

**Named vs implied vs missing** (do not promote the last column into a new pack):

| Storefront the user named | In the normative matrix | Meaning |
| --- | --- | --- |
| Novels | Named (`RW-BOOK-1`) | Already a job. Foreign reader. |
| 追番 / 美剧 | Implied by “watch a video library” + watchlists | Not a named episode/season product. |
| Sticker / emoji managers | Missing | Files are generic Inbox images. |
| Quote / memory vaults | Missing | Files + `NOTE`/`TAG`, not first-class quote subjects. |

**Checkout honesty** (requirements name experiences; this tree is thinner):

| Domain | Named | On this checkout |
| --- | --- | --- |
| Audio | `RW-AUDIO-1` player slice | Catalog + OpenSubsonic. Not a player. Cover is a placeholder. |
| Books | `RW-BOOK-1` reader | EPUB OPF + OPDS. Not a reader. No PDF/CBZ processor. |
| Video | `RW-VIDEO-1` | Generic files. No `video.list`. OpenSubsonic `getVideos` is an empty handshake. |
| Images | `RW-IMAGE-1` | Generic files. No EXIF / thumbnail pack. |

Green tests are not `RW-MVP-1`.

## 2. User seeds

These are the three storefronts that prompted the enumeration. They collapse onto jobs the matrix already has.

### 2.1 Image — stickers, memes, emoji packs, screenshots

Observed foreign apps (primary pages, not an endorsement; star counts omitted):

- [Sticker Foundry](https://github.com/saitatter/sticker-foundry) — self-hosted WhatsApp pack editor + Android ContentProvider.
- [Stickr](https://github.com/melvinchia3636/stickr) — React Native pack manager + local FFmpeg conversion server.
- [StickerBridge](https://github.com/ThijmenGThN/stickerbridge) — Telegram pack → Signal ZIP.
- [stikman](https://gitlab.com/sinanmohd/stikman) — desktop pack picker across chat programs.
- [meme-search](https://github.com/neonwatty/meme-search) — self-hosted semantic meme finder.
- [MemeLord](https://github.com/l4rm4nd/MemeLord) / [Memelet](https://github.com/toomanynights/memelet) — tagged meme boards.
- [Hydrus](https://hydrusnetwork.github.io/hydrus/introduction.html) — hash-identity, tag-not-folder archive (the collection job, not a chat picker).
- [sticker-convert](https://github.com/laggykiller/sticker-convert) / [QQmeme](https://github.com/I-Mortals/QQmeme) — convert/upload and folder+clipboard pickers.

Demand that changes the grade (not star counts): [Immich’s own backup doc](https://github.com/immich-app/immich/blob/master/docs/docs/administration/backup-and-restore.md) says Immich is not a filesystem backup — originals *and* Postgres are required, and the library folder is not recovery authority. NAS threads already treat photos as irreplaceable and movies as replaceable ([V2EX NAS backup](https://www.v2ex.com/t/1071996)). Chinese 表情包 workflow is “folder + clipboard app”; the durable job is the folder.

| Job | Grade | Plane | Foreign app still owns |
| --- | --- | --- | --- |
| Keep a personal meme / sticker corpus with exact bytes | **A** | Ingest, SHA-256, tags, CLIP-class search, restore | Nothing required |
| Find “the crying cat I saved in 2023” | **A** | Lexical + optional multimodal generation | Optional captioner |
| Dedup the same WebP saved from five group chats | **A** | `ContentID` / `same_content` | Perceptual near-dup is later |
| Tag a pack as `wechat` / `telegram` / `reviewed` | **A** | `TAG` | Pack UI |
| Recover after Immich / PhotoPrism / Paperless catalog loss | **A** then **B** | Exact originals + portable annotations | Rebuild *their* thumbs / ML DB |
| Bind cover-art bytes to a music/book subject | **B** | `SubjectRef` + later isolated extract | Tag writers, player chrome |
| Export a folder of PNGs into a WhatsApp-ready pack | **B** | Restore or `content.read` | Size limits, tray icon, ContentProvider |
| Convert Telegram → Signal / LINE | **C** | Do not | Format pipelines |
| Live sync into WeChat / WhatsApp sticker stores | **D** | Do not | Chat vendors |
| Face album / social photo stream | **D** | Do not | Immich-class product |

**Leverage:** treat stickers as images in the mixed Inbox. Do not ship a pack editor. A later image processor (EXIF, thumbnail, optional CLIP) is `RW-IMAGE-1`, not a sticker product.

### 2.2 Video — 追番, 美剧, movies, downloaded libraries

| Job | Grade | Plane | Foreign app still owns |
| --- | --- | --- | --- |
| Keep exact rips / remuxes with original paths | **A** | Snapshot, verify, restore | Nothing required |
| Resume after remux, rename, or player replacement | **A** | `PROGRESS` on `SubjectRef`, not a player GUID | Next-up UI |
| Keep `.srt` / `.ass` / NFO sidecars with the exact file | **A** | Same snapshot; sidecars are bytes, not video identity | Bazarr fetch, mux, style |
| Find a show by folder, filename, or note | **A** | Lexical search, tags | TMDB scrape |
| Watch from a restored tree or range read | **B** | `content.*` / restore | Infuse, IINA, mpv, VLC |
| Browse as a TV library with posters | **B** | Subject + annotations | Jellyfin / Plex / Emby *as clients*, not as our core |
| Rename / scrape TMDB / Bangumi metadata | **C** | Optional later EXTRACT | FileBot, TinyMediaManager, scrapers |
| Download tonight’s episode / RSS / indexer | **D** | Later `RW-RETRIEVE-1` only | Sonarr, Radarr, qBittorrent |
| Live transcode farm / HLS packaging | **C/D** | Do not embed | Jellyfin |

Split three jobs that one “追番 app” usually glues together:

1. **Keep the file forever** — ours (A).
2. **Play it tonight** — foreign player (B).
3. **Fetch the next episode** — not the plane (D / later retrieve).

OpenSubsonic is music-shaped. There is **no video HTTP facade** on this checkout. Do not invent a private `/rest/identify.view` or a Jellyfin API to “support 追番.” Watch is restore / `content.*`, then a foreign player.

Demand that changes the grade: Jellyfin 10.11 dropped watched/favorite across remux/rename ([#15001](https://github.com/jellyfin/jellyfin/issues/15001)). That raises plane-owned `PROGRESS`; it does not justify embedding a player. Some operators treat *arr video as re-downloadable and only protect photos/docs — so “keep rips forever” is the irreplaceable subset (fansubs, disc remuxes), not every Sonarr library.

### 2.3 Text — novels, quotes, memories, clippings

| Job | Grade | Plane | Foreign app still owns |
| --- | --- | --- | --- |
| Keep novels / EPUB / TXT / markdown trees | **A** | Ingest, UTF-8 / EPUB extract, FTS | Reader |
| Search a sentence you barely remember | **A** | Lexical today; bundled local semantic generation in the qualified target (fixture only in this checkout) | Nothing required |
| Keep reading position | **A** | `PROGRESS` via OPDS / Inbox | KOReader UI. Private sync protocol is **D** |
| Drop a quote / 语录 / temporary memory as a small file + note | **A** | Inbox + `NOTE` / `TAG` | Capture UI (share sheet stays foreign) |
| Import a chat export / Kindle clipping / email mbox | **A** | Exact tree + text extract | Export from the chat app |
| Keep a screenshot-of-text image | **A** | Exact bytes + tag | OCR / crop. Text-find unverified until OCR |
| Daily PKM / bidirectional links / daily notes | **C** | Do not | Obsidian, Logseq, Joplin |
| Flomo / memos-style stream editor | **C** | Do not | Those apps |
| Calibre conversion / cover-edit lab | **C** | Sit *under* a Calibre folder | Metadata editor, format convert |
| Cloud novel site / DRM store | **D** | Do not | Rights |

Do not collapse three lanes:

| Lane | Plane fit | Typical miss |
| --- | --- | --- |
| Archival corpus (novels, TXT dumps, chat exports) | **A** keep/find/restore; **B** OPDS to KOReader | Treating Calibre or the chat app as recovery authority |
| Daily capture (语录, 回忆, clippings) | **A** only on **exports** | Becoming Flomo (**C**); live highlight sync (**D**) |
| Knowledge graph (Obsidian / Logseq) | **A** = off-sync snapshot; graph itself **C** | Indexing the vault as if it were the product |

Demand: Obsidian’s own help says sync is not backup. Flomo documents HTML export and is not a long-document archive. WhatsApp chat export is not re-importable into WhatsApp. The costly pain is “sync ate the only copy” and “the export is not findable years later,” not another editor.

**Leverage:** a folder of `.txt` / `.md` / EPUB plus tags is already a quote vault. Do not become Obsidian. OPDS is the book attach point; Inbox is the mixed attach point.

## 3. Adjacent jobs one Inbox already covers

These were easy to miss because they are not the three seeds. Most do **not** need a new public API.

| Job | Grade | Notes |
| --- | --- | --- |
| Personal music library | **B** | Strongest facade today (OpenSubsonic) |
| Comics / manga CBZ | **B** | Already under books/comics in the matrix |
| Podcasts / audiobooks as files | **A/B** | Files + progress; player stays foreign |
| Phone camera-roll *tree* (a copy, not live Photos.app) | **A** | Ingest the dump; do not sync the phone |
| Family scans, tax, legal PDFs | **A** | High pain; privacy; no OCR required to keep |
| Courseware / lecture recordings | **A/B** | Mixed video + PDF in one tree |
| Voice memos | **A** | Exact + optional later ASR artifact |
| Research papers / Zotero library export | **A/B** | Keep PDFs; Zotero stays the citation app |
| Telegram / WeChat / iMessage *exports* | **A** | Archive the export; do not bridge live chat |
| RAW camera + selects | **A** | Exact masters; Lightroom stays foreign |
| Design assets, fonts, SVG | **A** | Generic subjects |
| Game saves / mods / install trees | **A** | Locate and restore; do not launch |
| “Park this zip between laptops” | **A** | Temporary is still exact |
| Homelab compose / config trees | **A** | Small, high regret if lost |
| Active torrent swarm | **D** | Completed library is A; the swarm is not |
| Writable NAS / SMB workshare | **D** | Closed: not this product |

Jobs that look popular but fail Occam (restored folder + foreign tool already does it):

| Ask | Already exists |
| --- | --- |
| “Make it mountable” | `plan.restore` + rclone / sshfs / NAS SMB |
| Photo library / faces | Immich / PhotoPrism on a restored DCIM |
| Paperless DMS | paperless consume dir after restore |
| Device parking / sync | Syncthing; admit a *settled* drop folder only |
| Torrents / *arr | qBittorrent until complete; then ingest |
| Game launcher | Lutris / Playnite on a restored tree |
| Second REST catalog / native suite | Closed. Inbox is not a WebUI |

## 4. What this checkout must not become

Repeated because every seed above has a C/D twin:

- No RestoreWeave sticker / meme / emoji product.
- No RestoreWeave 追番 / Sonarr / BitTorrent product.
- No RestoreWeave Flomo / Obsidian / 语录 editor.
- No embedded player, reader, or media server in `restoreweaved`.
- No fourth public REST catalog.
- No FUSE/SMB/WebDAV server so those apps can mount us.

If an operator wants Immich, Jellyfin, KOReader, or Sticker Foundry, they attach to HTTP APIs or to a **restored directory**. We keep identity and recovery.

## 5. Highest-leverage list (keep / find / restore is the pain)

Ordered by how much the plane already does without a new subsystem:

1. Mixed Inbox of unknown junk (the actual product).
2. Personal music (OpenSubsonic clients).
3. Novels / comics / markdown (OPDS + text extract).
4. Exact video rips + `PROGRESS` that survives remux (player stays foreign).
5. Meme / sticker / screenshot *files* (not a pack manager or picker).
6. Quote / memory / chat-export / Flomo-export text (not a PKM).
7. Family scans and tax PDFs (Immich/Paperless still need a backup).
8. Phone camera-roll *dumps* and lecture trees (`snapshot.diff`, not Photos.app).
9. Completed download libraries after the swarm dies.
10. Cover-art / sidecar bytes bound to the same subject; restore then hand off.

Items 5–8 are the user’s seeds plus the obvious neighbors. They do not move `RW-MVP-1`. They do not need new `rw` subcommands.

## 6. How a foreign app should attach

```text
keep / find / tag / prove     -> Inbox JSON or rw / MCP
listen                        -> OpenSubsonic
read a book                   -> OPDS
play a video or open a pack   -> restore or content.* then the foreign app
download / edit / chat-sync   -> not our surface
```

One tree can be a sticker dump, a 美剧 folder, and a 语录 directory at once. Membership is additive. Recovery meaning stays SHA-256.

## 7. What would reverse a row

- A standard HTTP API for “sticker packs” or “TV libraries” that we could adapt the way we adapted OpenSubsonic — then a thin facade is B, not a new product.
- A retrieve profile that is separately qualified — then “get the next episode” can be D → later, never identity.
- Evidence that operators will not use Inbox / restore and will only adopt a native suite — that still does not authorize us to become the suite; it would be a failed attach, not a new core.
- Operators refuse to ingest a tree unless we also edit or read inside `restoreweaved`. That would be a product-identity change, not a missing text feature.
