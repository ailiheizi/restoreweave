# Ecosystem and Vertical Application Requirements

> **Authority:** This is the normative product requirement for the RestoreWeave ecosystem and vertical-application profile. It governs the shared catalog, Inbox, experience-pack boundary, client strategy, and staged domain experiences. The companion [Universal Catalog and Experience Pack Overview](universal-catalog-and-experience-packs.md) is non-normative and summarizes this document for orientation. The technical contracts refine these requirements but must not weaken their recovery and authority rules.

> **Profile status:** This document defines the product shape above `RW-MVP-1`. The bundled local text-semantic profile is already an `RW-MVP-1` requirement; this document does not promote additional semantic or multimodal processing, media players, readers, or game retrieval into the core release. It explains how later experiences can share one RestoreWeave data plane without turning the authoritative core into a media server, launcher, workflow engine, or download manager.

## 1. Product decision

RestoreWeave should support an ecosystem of focused applications over one content and recovery layer. The common layer owns identity, exact storage, namespace, provenance, annotations, access, verification, and typed search. Domain applications add interpretation and presentation for music, video, books, photographs, documents, software, games, datasets, and unfamiliar files.

The product must therefore be **one data plane, several bounded experiences**:

```text
RestoreWeave Core
  -> Universal Catalog and Inbox
  -> Domain Packs (audio, video, books, images, documents, games, ...)
  -> Clients (CLI, local MCP, export consumers, later WebUI and focused applications)
```

The public product hierarchy is intentionally layered rather than a set of separate storage silos:

```text
RestoreWeave Core        identity, namespace, storage, recovery, authorization
RestoreWeave Catalog     heterogeneous metadata, annotations, collections, baseline search
RestoreWeave Experiences Music, Books, Video, Photos, Documents, Games, and other views
RestoreWeave Retrieval   explicitly qualified external acquisition connectors (later profile)
RestoreWeave Clients     universal UI, CLI, MCP, export consumers, and third-party applications
```

The shared catalog has four durability layers:

| Layer | Examples | Durability |
| --- | --- | --- |
| Core subject facts | `SubjectRef`, path, snapshot, exact digest, size, type evidence, representations, verification | Authoritative and recovery-relevant |
| Common catalog facts | Tags, notes, source, timestamps, duplicate groups, processing state, minimal link groups, and saved views | Authoritative user/catalog data |
| Typed domain artifacts | Audio tags, video streams, book chapters, EXIF, game membership evidence | Versioned and provenance-bound; rebuildable unless explicitly promoted |
| Search projections | Lexical, vector, visual, acoustic, graph, and recommendation indexes | Rebuildable and generation-pinned |

One file may participate in several experiences at once. A directory may be an album, a photo set, a source tree, and an ordinary path. Membership is additive; opening or restoring a subject always resolves through the canonical namespace and content-access contracts.

“One product” does not mean one binary containing every parser, codec, model, launcher, and external service. It means that every experience refers back to the same `SubjectRef`, `ContentRef` (`ContentIdentity` in product language), `RepresentationRef`, `SnapshotId`, `SegmentRef`, annotation, and recovery evidence. A user can search, open, annotate, play, read, inspect, or restore an item without copying it into a second catalog with a different identity model.

## 2. User promise

The ecosystem should make the following workflow feel natural:

```text
put almost anything into an Inbox or attach an existing tree
-> protect exact bytes first
-> identify and extract what can be understood
-> show useful status and suggestions
-> find it through one catalog
-> open it in the best available domain experience
-> keep annotations, progress, and collections attached to stable subjects
-> restore the original path and bytes with proof
```

Suggested public language:

> **Put anything in. Find it later. Use it where it belongs. Restore with proof.**

The system should not promise that every file will receive a rich player or perfect semantic description. Unknown and unsupported files remain useful as exact, searchable, inspectable objects with visible processing status.

### 2.1 Practical use-case matrix

The matrix below translates the ecosystem idea into concrete jobs rather than a list of applications. **Immediate value** is what the user can use before optional enrichment finishes. A **domain lens** is a replaceable interpretation and client over the same host-owned `SubjectRef`; it is not a second catalog or storage authority. The phase names are delivery profiles, not claims that those clients already ship.

| User/job | Typical input | Immediate value | Domain lens | Core APIs / contracts | Phase | Main risk and guardrail |
| --- | --- | --- | --- | --- | --- | --- |
| Drop mixed material into one Inbox | Watched folder, upload, or attached tree containing known, unknown, and duplicate files | Exact capture and hashing; generic path/type/duplicate status; searchable item page even when parsing fails | Universal Inbox / generic catalog | `plan.ingest`; host suffix/magic evidence; optional `Processor.CLASSIFY_LEARNED`/`PARSE`/`EXTRACT`; `search.query`; `SubjectRef`; annotations | `RW-CATALOG-1` | AI or parser mistakes could hide content. Exact ingest is independent; suggestions are reviewable proposals. |
| Import a NAS tree without racing active writers | Existing root, periodic rescan, incomplete upload, or a file that is still being copied | Capture consistency, settle/debounce status, changed-only work, and a snapshot diff | Namespace and catalog | `CaptureDriver`; `CaptureRef`; `plan.ingest`; `status.get`; `snapshot.diff` | `RW-CORE-1` / `RW-CATALOG-1` | A partial file may be mistaken for a final version. Require stability evidence, retry, explicit uncertainty, and source-retention policy. |
| Reduce managed storage without losing recovery meaning | A mixed corpus with duplicates, compressible files, and optional transform candidates | A scoped savings waterfall, admitted representation candidates, and a reviewable plan showing exact/reversible versus approximate outcomes | Storage policy and representation planner | `plan.ingest`; `RepresentationRef` (`RepresentationId` in compact API names); `RepositoryDriver`; `Processor.TRANSFORM`/`VALIDATE`; `snapshot.verify` | `RW-CORE-1` / later protection profile | Apparent savings can be erased by indexes, thumbnails, models, caches, or retained sources. Count all overhead and never retire the exact fallback without a separately qualified policy. |
| Keep the catalog current after edits | Changed files, renames, moves, deletes, watcher events, or periodic rescans | Incremental snapshots, stable `AssetRef` continuity, and explicit added/removed/changed status | Lifecycle and catalog reconciliation | `CaptureDriver`; journal/checkpoint; `plan.ingest`; `snapshot.diff`; `AssetRef`; later `plan.retention`/`plan.gc` | `RW-CATALOG-1` / later lifecycle profile | Lost events or source drift can create false deletions. Treat watchers as hints, require a complete baseline after continuity loss, and block tombstones while coverage is uncertain. |
| Find an arbitrary item later | Filename, path, suffix, magic type, digest, extracted text, tag, or note | Baseline lexical/structured search with facets and a direct path or subject result | Generic catalog | `search.query`; `namespace.resolve`/`stat`; generation-pinned `QueryProvider` | `RW-CATALOG-1` | Stale indexes or unauthorized snippets can mislead or leak. Expose freshness/coverage and reauthorize every result. |
| Open, export, or restore with proof | A selected subject, snapshot, path, or recovery manifest | File-shaped browse, bounded range reads, verification, and original-path reconstruction | Core file access | `namespace.*`; `content.open`/`read`; `snapshot.verify`; `plan.restore`/`apply`; `recovery.export` | `RW-CORE-1` | A preview or approximate rendition could be mistaken for the master. Exact access is the default and every fidelity claim is explicit. |
| Listen to a personal music library | FLAC, MP3, OGG, tags, artwork, multi-disc albums, and playlists | Album/track browse, seekable streaming, playlists, progress, and duplicate candidates | Audio pack and player | `Describe`; `ListSegments`; `Open`/`Read`; `CollectionRevision`; annotations; optional `decentralized-music` or Chromaprint artifacts | `RW-AUDIO-1` | Tag conflicts, codec gaps, and acoustic near-matches. Preserve source tags, qualify codecs/transforms, and label similarity as approximate evidence. |
| Read novels, books, or comics | EPUB, PDF, CBZ, Markdown, and other qualified formats | Chapters/pages, full-text search, progress, bookmarks, highlights, and notes | Books/reader pack | `ListSegments`; `content.open`/`read`; `search.query`; collections; annotations | `RW-BOOK-1` | DRM and rendering differences. Preserve originals, use bounded renderers, and do not imply DRM circumvention or universal format support. |
| Watch a video library | Containers with multiple streams, subtitles, chapters, and artwork | Metadata, thumbnails, direct range playback, resume, and a bounded fallback transcode | Video pack and player | Domain `Describe`/`ListSegments`; `Open`/`Read`; `Processor.TRANSFORM`; annotations | `RW-VIDEO-1` | Codec, CPU/GPU, seeking, and subtitle complexity. Use qualified profiles, quotas, and exact-source fallback. |
| Review photos and documents together | Image folders, scans, PDFs, office files, archives, and OCR candidates | Thumbnails, EXIF/text extraction, duplicate candidates, and one mixed search surface | Image/document packs | `Processor.EXTRACT`; `ArtifactRef`; `search.query`; `content.open`; annotations | `RW-IMAGE-1` and later document profile | Sensitive content, model egress, and derivative storage cost. Make semantic processing opt-in, label coverage, and retain provenance. |
| Locate applications and games without launching them | App bundles, manifests, saves, mods, DLC, runtimes, and caches | Version/platform inventory, component graph, missing-component checklist, and restore context | Application/game resolver | Capture and extraction contracts; `RelationRef`; `snapshot.diff`; search; collections | `RW-COLLECTION-1` | Platform drift, secrets, DRM, and unsafe execution. Start read-only; separate inventory, restore planning, retrieval, and execution capabilities. |
| Organize and annotate without moving bytes | Tags, notes, ratings, playlists, reading/watch lists, corrections, and multiple users | Virtual collections and durable user state that survive index rebuilds and client replacement | Catalog and domain annotations | `annotation.list`/`upsert`/`delete`/`export`/`import`; `CollectionRevision`; ACL and optimistic revisions | `RW-CATALOG-1` then domain profiles | Concurrent edits, privacy, and accidental tombstones. Bind revisions to subjects, preserve visibility, export history, and require authorization. |
| Ask an external AI to classify or prepare a review plan | Natural-language query, selected subjects, processor profile, or a proposed organization task | Suggestions, summaries, tags, duplicate explanations, and ranked results without hidden mutation | CLI/MCP and external harness | Read-only CLI/MCP operations; `Processor`; `QueryProvider`; proposal and plan contracts | `RW-CATALOG-1` / optional `RW-SEMANTIC-1` | Prompt injection, exfiltration, and over-broad mutation. Mediate all access through grants; default to read-only and require human apply. |
| Rebuild or upgrade an index/processor | Stale projection, changed parser, new embedding model, or moved repository | Generation-pinned rebuild while identity, annotations, and restore remain usable | Artifact/index lifecycle | `IndexProvider`; `QueryProvider`; `IndexGenerationRef`; `capability.list`; `status.get`; export/import | `RW-CATALOG-1` / `RW-SEMANTIC-1` | Model drift and index growth can erase savings. Keep artifacts rebuildable, pin generations, show coverage, and account for all overhead. |
| Work from a portable or intermittently connected copy | Recovery closure, detached media, offline cache, or a second NAS | Browse manifest, verify selected content, restore, and optionally replay annotations | Offline/portable client | `recovery.export`; `snapshot.verify`; `content.open`/`read`; annotation export/import | `RW-CORE-1` plus later offline profile | Stale caches, divergence, and leaked credentials. Use immutable manifests, bounded caches, explicit sync/reconciliation, and scoped handles. |
| Compare versions and clean up safely | Two snapshots, duplicate candidates, retention proposal, or moved source | Added/removed/changed paths, content savings evidence, and a reviewable operation plan | Core lifecycle and catalog | `snapshot.diff`; `plan.revise`; later `plan.retention`/`plan.gc`; annotations | `RW-CORE-1` / later lifecycle profile | False duplicate or premature deletion. Keep early cleanup non-destructive; require immutable plans, reachability checks, and human apply. |
| Reacquire a missing or optional game/media source | Approved URL, package provider, store binding, or future magnet/P2P source | Candidate source, expected digest, quarantine, and independent validation plan | Retrieval profile | `DISCOVER -> PLAN_RETRIEVAL -> ACQUIRE -> EXECUTE`; `RetrieverDriver`; content validation | `RW-RETRIEVE-1` (later) | Rights, malware, source drift, and confusing availability with recovery. Separate capability and credentials; require approval, quarantine, digest checks, and an exact local fallback. |

This matrix implies a deliberately asymmetric product shape: the first rows must work with only the core and baseline catalog, while later rows add focused lenses. If a lens is absent or fails, the item remains a generic, exact subject that can be searched, inspected, and restored. The ecosystem is therefore one authoritative content plane with many optional experiences, not one application that must implement every player, parser, model, downloader, and launcher before it is useful.

## 3. Shared platform versus domain behavior

### 3.1 Shared platform capabilities

These capabilities should be implemented once and reused by every vertical:

- Exact content identity, length, digest, and duplicate relationships.
- Immutable snapshots and original-path namespace reconstruction.
- Bounded `FileAccess` and range/stream reads.
- Exact, reversible, derived, and approximate representation records.
- Capture, placement, publication, verification, restore, and lifecycle evidence.
- Generic metadata, type evidence, processing state, and failure reasons.
- Versioned tags, notes, ratings, progress markers, and user corrections.
- Minimal file-only LinkGroups and saved queries that do not require physical file moves.
- Baseline lexical, structured, and bundled local text-semantic search.
- Optional additional semantic and multimodal artifacts bound to stable subjects or segments.
- Capability discovery, processor provenance, authorization, and resource limits.
- CLI and local read-only MCP operations; later REST and WebUI adapters after the core command and access contracts are qualified.

### 3.2 Domain-specific capabilities

These should remain outside the authoritative kernel and be supplied by a domain pack or client:

- Parsing a media/container/document format.
- Extracting domain fields and structural segments.
- Generating previews, waveforms, thumbnails, subtitles, or page images.
- Choosing a playback, reading, or inspection presentation.
- Domain-specific facets, sorting, recommendations, and collection rules.
- Optional transcoding or quality assessment.
- Application/game membership and dependency resolution.
- External source binding and reacquisition.

The first implementation may bundle selected domain processors and clients. A public marketplace or arbitrary third-party plugin ABI is not required until at least two independent implementations demonstrate that the seam is stable and secure.

## 4. Common object model

Every domain experience should use the following host-owned objects:

| Object | Role in the ecosystem |
| --- | --- |
| `SubjectRef` | Stable target for search results, annotations, access, and UI navigation. |
| `ContentRef` (`ContentIdentity`) | Exact bytes, independent of source path or physical placement. |
| `AssetRef` | Optional stable continuity for a user-facing asset across accepted renames, replacements, or versions. |
| `RepresentationRef` (`RepresentationId`) | One exact, reversible, derived, or approximate representation with provenance and a fidelity claim. `RepresentationRef` is the catalog reference; `RepresentationId` is its identifier field. |
| `SegmentRef` | A bounded part of a subject, such as a track, chapter, page, image region, subtitle span, or video time range. |
| `CollectionRef` | A virtual album, playlist, reading list, tag set, game library, or saved query. |
| `Annotation` | Durable user-authored tag, note, rating, correction, bookmark, or progress record. |
| `ArtifactRef` | A rebuildable processor output such as extracted text, thumbnail, transcript, waveform, fingerprint, or embedding. |
| `SnapshotId` | An immutable namespace and recovery view. |
| `AccessHandle` | A scoped read or stream capability; it is not a repository locator. |

Domain schemas add namespaced fields rather than redefining these objects. Examples include `audio.artist`, `video.duration`, `book.author`, and `game.platform`. A missing domain pack must not make generic browse, search, or exact restore unavailable.

### 4.1 Work, edition, release, and exact content

The catalog must not collapse a human concept into a byte identity. Domain packs may relate content through a four-level concept ladder:

| Level | Meaning | Example |
| --- | --- | --- |
| **Work** | The abstract intellectual or creative work | A novel, composition, film, game, or software product |
| **Edition** | A materially distinct editorial, regional, platform, language, or mastering form | A translated novel, remaster, director's cut, or platform edition |
| **Release** | One distributed publication/build/package of an edition | EPUB release, album pressing, Blu-ray release, game build/depot |
| **Content identity** | One exact byte sequence observed or admitted by RestoreWeave | SHA-256 plus exact length for a specific file/blob |

`Work`, `Edition`, and `Release` are domain/catalog identities supported by evidence and provenance. They can group several exact files and may be corrected or disputed without changing recovery records. `ContentIdentity` is immutable byte truth: two releases with the same title are not exact matches, and two different container files may belong to the same work without being interchangeable. An `AssetRef` may provide user-facing continuity across accepted revisions, but it never proves byte equality or authorizes replacement.

### 4.2 Experience-pack contract

An experience pack is a versioned package containing some combination of:

- domain schemas and artifact definitions;
- `Processor`, `IndexProvider`, and `QueryProvider` capability profiles;
- preview, rendition, or stream request profiles;
- client routes, presentation metadata, and user-state schemas;
- conformance fixtures, migration rules, and license/dependency declarations.

The host brokers all access. A pack must not read repository-private packs, SQLite tables, ambient host paths, or unrestricted network/process state. It declares supported classes and formats, minimum exact and derived capabilities, graceful fallbacks, decoder/runtime dependencies, privacy and egress requirements, whether each action is read-only, proposal-producing, or mutation-capable, and the portability guarantees for its user data. The reference distribution may bundle selected packs; an arbitrary third-party marketplace is not required before the seam is proven by independent implementations.

### 4.3 Content access and renditions

Players, readers, and other clients request a scoped `AccessHandle` containing a stable subject/revision, an authorized representation or rendition policy, byte-range or stream mode, principal and expiry, quotas and cancellation, integrity evidence, and decoder requirements. They never receive repository paths or ambient host paths.

The core selects the original exact representation or an admitted, validated derivative according to the client's fidelity needs. A thumbnail, waveform, subtitle, transcript, normalized text, embedding, or transcode is a rebuildable derivative and cannot silently become the only recovery path. On-demand transforms are bounded, cacheable, auditable, and invoked through a qualified processor profile.

The access contract should support random range reads, seekable streams, progressive previews, rendition negotiation (codec, resolution, bitrate, or text format), cache hints, and readback digest or segment-integrity evidence. A failed transform degrades playback or preview only; it never changes exact identity or restore truth.

## 5. Universal Inbox and heterogeneous catalog

### 5.1 Why this is the first ecosystem feature

The “drop miscellaneous files here and index them” workflow has the widest coverage and the highest leverage. It exercises the core identity, processor, index, annotation, and access contracts without requiring a complete media player or application resolver. It also gives every later vertical a common arrival path.

The user-facing name should be **Inbox**, **Dropzone**, or **Unsorted**, not “junk.” “Junk” implies that the system may discard material; the default behavior is preservation and review.

### 5.2 Inbox behavior

The initial Inbox is a bounded import and review profile, not a writable NAS filesystem. It may watch or scan a source tree, but it does not expose SMB/NFS/WebDAV write semantics, silently reorganize the source, or treat live mutation as application-consistent capture. A later writable-NAS profile must define its own consistency, locking, conflict, rollback, and authorization gates.

An Inbox profile should:

1. Accept a watched directory, import root, upload, or read-only source view.
2. Record the original path, source, arrival time, and capture consistency.
3. Settle or snapshot a source according to its declared capture profile before treating observations as one import batch. A live watched tree uses a bounded quiet period and mutation checks; files that keep changing remain pending or are captured as separate revisions.
4. Perform exact ingest independently of classification or AI availability.
5. Run suffix, magic-byte, and bounded parser evidence in order.
6. Mark files as `identified`, `partially_understood`, `duplicate`, `needs_review`, `unsupported`, `sensitive`, or `processing_failed` without hiding the exact item.
7. Extract common metadata and text where safe; retain failures and coverage.
8. Offer suggested tags, destination collections, renames, or deduplication decisions as reviewable proposals.
9. Permit virtual organization before any physical move or source-retirement decision.
10. Expose one universal item page: path, type evidence, hashes, versions, storage facts, derived artifacts, annotations, related items, and restore action.
11. Preserve an exportable manifest so the Inbox can be rebuilt or moved.

Automatic file movement, destructive deduplication, source deletion, or AI-only classification is out of scope for the initial Inbox profile.

### 5.3 Generic item states

The catalog should make these states visible and separable:

- Protection: exact, fallback, blocked, or explicitly unprotected.
- Understanding: unknown, identified, parsed, partially parsed, or conflicting.
- Discovery: not indexed, indexed, stale, failed, or approximate-only.
- Access: exact-readable, derived-readable, unavailable, or authorization-blocked.
- Organization: inbox, reviewed, assigned to one or more virtual collections, or archived.

These are not one overloaded status field. A file may be exact and recoverable while its semantic index is stale or its media preview is unavailable.

### 5.4 Processing tiers

The controller should expose processing as explicit tiers so operators can trade latency, cost, and enrichment without changing recovery truth:

| Tier | Purpose | Typical work | Required behavior |
| --- | --- | --- | --- |
| **Protect** | Make the item safe and addressable | Source settle/snapshot, exact hashing, duplicate identity, namespace record, baseline type evidence | Synchronous or highest priority; cannot depend on AI or optional processors |
| **Understand** | Build useful generic and typed metadata | Magic-byte/structural parsing, common metadata, safe text extraction, archive/member listing | Bounded and asynchronous; failure leaves exact browse/read/restore intact |
| **Discover** | Improve retrieval and cross-modal navigation | Bundled local text embeddings for the default profile; OCR, ASR, fingerprints, CLIP-compatible features, and external enrichment as extensions | The local text profile is required for the default experience; extensions are attributed, privacy/egress governed, rebuildable, and generation-pinned |
| **Transform** | Produce a qualified access or storage representation | Lossless/reversible packing, thumbnails, normalized text, bounded transcodes, approved perceptual representations | Requires an explicit fidelity/retention profile and independent validation; never silently replaces exact content |

Tier completion is reported independently. An item can be Protect-complete while Understand or Discover is pending, stale, or failed. Resource reservations must protect the Protect and interactive-read lanes from optional enrichment backpressure.

### 5.5 Universal item page

Every result and namespace entry should open one common item view that exposes:

- observed paths, snapshots, versions, exact identity, size, and type evidence;
- duplicate relationships, representations, placements, verification, and recovery state;
- tags, notes, collections, provenance, extracted text, and authorized typed metadata;
- related subjects, processor status, and active search-provider generation;
- safe actions such as open, range-read, stream, export, compare, verify, and restore.

When a domain pack recognizes the subject, the same view may offer actions such as **Play**, **Read**, or **Open in Music**. Each action resolves through a scoped `AccessHandle`; it is not a shortcut to repository-private state.

## 6. Domain pack profiles

### 6.1 Audio and music

**User value:** High for personal NAS libraries and a natural first specialized experience. Local playback has low legal and authority risk compared with external acquisition, and most common operations are read-only.

**Initial scope:**

- Parse common containers and tags using a pinned external tool or library.
- Extract artist, album, title, track number, year, genre, codec, bitrate, sample rate, channels, duration, and embedded artwork where available.
- Represent albums, discs, tracks, playlists, and compilation relationships as virtual collections or `SegmentRef` records.
- Provide bounded byte-range streaming and optional server-side transcode for a small, qualified codec set.
- Generate a waveform or loudness artifact only as a rebuildable derivative.
- Use Chromaprint or the user's `decentralized-music` algorithm as optional near-match/fingerprint evidence, never as exact identity.
- Preserve original tags and user corrections separately; do not silently rewrite source files.

**Defer:** multi-room synchronization, adaptive streaming, automatic lossy replacement, external music acquisition, DRM handling, and a new audio codec.

**Why early:** It can validate the common `FileAccess`/stream/segment/annotation model with comparatively contained complexity.

### 6.2 Video

**User value:** High, but operational complexity is materially higher than music.

**Initial scope:**

- Parse container and stream metadata with a pinned `ffprobe`/FFmpeg profile.
- Index duration, codecs, dimensions, frame rate, audio tracks, subtitles, chapters, language, and capture time.
- Generate thumbnails, contact sheets, subtitle text, and optional scene markers as rebuildable artifacts.
- Serve original bytes when the client supports them; use a bounded transcode profile only when required.
- Expose time-range `SegmentRef` values for chapters, subtitles, and future semantic results.

**Defer:** universal codec compatibility, live TV, continuous hardware-accelerated transcoding, HLS/DASH packaging, editing, and perceptual replacement. Video quality metrics may inform an explicitly approved derived representation but cannot replace exact bytes by default.

**Why later than music:** codec licenses, FFmpeg packaging, subtitles, seeking, range reads, CPU/GPU scheduling, and concurrent streams make this a larger qualification surface.

### 6.3 Books, novels, and comics

**User value:** Medium-high and technically approachable. A reader can be built over extracted structure without owning the storage layer.

**Initial scope:**

- Identify EPUB, PDF, CBZ/CBR, FB2, MOBI/AZW where legally and technically supported.
- Extract title, author, series, language, publisher, dates, identifiers, table of contents, page/chapter boundaries, and text where permitted.
- Bind chapters, pages, and text ranges to `SegmentRef` values.
- Provide full-text search, reading progress, bookmarks, notes, and highlights as durable annotations.
- Render original EPUB/PDF/CBZ through a client or bounded preview artifacts; preserve the original file as authoritative.

**Defer:** DRM circumvention, reflow guarantees across every format, cloud library synchronization, automatic translation, and generated summaries as authoritative metadata.

**Why early:** It extends the same extraction, segments, search, annotations, and read-only access contracts with less streaming complexity than video.

### 6.4 Images and photographs

**User value:** High, but the category has strong existing products. It is useful as a processor pack, not a reason to duplicate a full photo-management stack immediately.

**Initial scope:** EXIF/XMP extraction, dimensions, orientation, thumbnails, duplicate/perceptual candidates, OCR/caption artifacts when optional providers are installed, and date/location facets with privacy controls.

**Defer:** face recognition, automatic sharing, destructive normalization, and a complete photo social layer. An Immich or PhotoPrism integration can consume the same namespace later.

### 6.5 Documents and research material

**User value:** High for mixed NAS collections and teams.

**Initial scope:** MIME detection, archive inspection, bounded text extraction/OCR, author/title/date fields, tags, notes, full-text search, and source citations. Paperless-ngx, Tika, libarchive, and Tree-sitter are integration references rather than mandatory core dependencies.

**Defer:** collaborative editing, workflow automation, and document version-authoring semantics.

### 6.6 Applications and games

**User value:** Compelling but niche and high risk. The first useful feature is **locate and explain**, not download and launch.

**Phase 1 — inventory/resolution:**

- Detect likely applications, games, launchers, bundles, package manifests, save roots, configuration, mods, DLC, runtimes, and caches.
- Emit a versioned collection/component/dependency graph with evidence and confidence.
- Search by title, platform, version, launcher, save state, mod, and missing component.
- Keep all bytes on the generic exact route when resolution is incomplete.

**Phase 2 — restore planning:**

- Produce a human-reviewed component restore plan and identify what is exact, source-equivalent, blocked, or manual.
- Validate manifests, hashes, signatures, and dependency closure without launching content.

**Phase 3 — external retrieval:**

- Add a separately permissioned `RetrieverDriver` for approved provider APIs or package sources.
- Require immutable source binding, credentials/rights review, quarantine, cold acquisition, independent validation, and an exact local fallback.
- Treat magnet/P2P as one optional source mechanism, never as proof of persistence.

**Do not promise:** DRM bypass, account recovery, entitlement transfer, universal launcher compatibility, or safe automatic execution. The existing [Application and Game Collection Requirements](application-and-game-collections.md) remains the normative later profile.

## 7. Search and collection model

The universal catalog should provide one search entry point with typed, namespaced fields and graceful degradation:

```text
search(query)
  -> fused lexical, structured, and bundled local text-semantic baseline
  -> optional domain facets
  -> optional additional semantic or multimodal ranking
  -> authoritative SubjectRef and SegmentRef results
```

Domain packs contribute schemas and processors; they do not create separate source-of-truth databases. A result always links back to an authoritative namespace entry, exact content, or bounded derivative.

Collections are virtual by default. A user can create:

- Albums, playlists, podcasts, and “recently added” views.
- Reading lists, series, bookmarks, and unfinished books.
- Watchlists, seasons, and curated video groups.
- Photo projects, events, and review queues.
- Game libraries, platform views, mod sets, and restore checklists.
- Generic saved searches, tags, and Inbox queues.

Collections store membership and ordering over stable subjects. They do not duplicate bytes or authorize deletion. Physical path moves are a separate reviewed operation.

The `RW-MVP-1` baseline provides generic tags, notes, `SavedView`, and the
minimal file-only `LinkGroup` defined by the content-store contract. A
LinkGroup is one stable group subject plus its current map of safe
group-relative paths to stable file `SubjectRef` values. It is a composition
of links, not a second copy of the files and not a versioned collection
history. Richer virtual Collections and domain-specific lenses (albums,
reading lists, seasons, photo projects, game libraries, and similar views)
remain later profiles; they must remain portable across clients and MUST NOT
reinterpret the LinkGroup mapping.

## 8. Client and presentation strategy

### 8.1 Universal client first

The first qualified client should be the CLI plus local read-only MCP surface, with a small universal browse shell as an optional adapter. A full WebUI is a later profile after command, access, authorization, and generation-pinning contracts are stable; a prototype WebUI must not become an implicit `RW-MVP-1` dependency. The universal client must support:

- Inbox triage and status filters.
- Search and facets.
- Item detail with provenance, representations, annotations, and restore.
- Safe open/download/stream through scoped handles.
- Virtual collections and saved queries.

### 8.2 Specialized clients second

Music players, video players, and book readers should be separate clients or bounded UI modules that call the same typed access and catalog APIs. They should not read repository packs, SQLite tables, or host paths directly. A specialized client may offer domain actions such as `play`, `read`, `bookmark`, or `queue`, but each action resolves to canonical subject/segment/annotation operations.

This allows a single server to serve multiple clients and allows an operator to replace a client without migrating storage or metadata.

### 8.3 AI and external harnesses

An external AI client can call read-only CLI/MCP operations to:

- Search and summarize authorized items.
- Suggest tags, collections, metadata corrections, or processing profiles.
- Explain duplicates, storage savings, and unresolved files.
- Generate a reviewable plan.

It cannot silently organize, delete, retrieve, or publish. A future mutation-capable adapter requires explicit authority and a separate profile.

## 9. Release sequencing

| Stage | User-visible outcome | Required shared capabilities | Priority |
| --- | --- | --- | --- |
| `RW-CORE-1` | Exact archive, verify, restore, original-path browse | Core identity, repository, publication, `SnapshotTree`, `FileAccess` | Must ship first |
| `RW-CATALOG-1` | Drop anything into an Inbox and find it | Generic extraction, the qualified fused default search, durable descriptions/annotations, virtual collections, universal item page | First post-core product wedge |
| `RW-AUDIO-1` | Browse and stream a personal music library | Audio parser, track/album segments, stream handles, playlists, progress | Early vertical |
| `RW-BOOK-1` | Search and read books/comics with progress and notes | EPUB/PDF/CBZ processors, chapter/page segments, reader client | Early vertical |
| `RW-VIDEO-1` | Browse and play a video library | FFprobe/FFmpeg profile, thumbnails, subtitles, time segments, transcode policy | After core streaming qualification |
| `RW-IMAGE-1` | Search and review photos/images | EXIF/XMP, thumbnails, duplicate candidates, optional vision processors | Parallel or integration profile |
| `RW-COLLECTION-1` | Explain installed apps/games and their components | Platform profiles, collection resolver, dependency graph, save/mod handling | Later |
| `RW-RETRIEVE-1` | Approved external acquisition/reacquisition | `RetrieverDriver`, source bindings, quarantine, rights, cold tests | Later and opt-in |
| `RW-SEMANTIC-1` | Visual, cross-modal, and other semantic discovery beyond the bundled local text profile | CLIP/ASR/OCR processors, additional embedding spaces, vector/hybrid index generations | Optional after the bundled local text profile |

This vertical sequence begins only after the dependency-ordered [Core MVP Execution Plan](../technical/core-mvp-execution-plan.md) passes. The first post-core demonstration should be `RW-CATALOG-1` over mixed files, followed by a small `RW-AUDIO-1` slice. Neither Inbox work nor a domain client may substitute for recovery closure, the real local semantic default, qualified storage, saved views/exports, or release acceptance.

The sequence distinguishes the narrow import/catalog path from later writable-NAS behavior: `RW-CORE-1` and `RW-CATALOG-1` may observe local or mounted trees and publish a managed exact archive, but they do not expose a general writable network filesystem. SMB/NFS/WebDAV write-through, source-side organization, in-place edits, application-consistent capture, and multi-writer conflict resolution require separately named NAS profiles.

## 10. Product metrics

Evaluate the ecosystem as a daily content product, not only by compression ratio:

- time from Inbox drop to baseline searchability;
- percentage of observed bytes with exact protection and a usable original-path read;
- net physical savings after repositories, indexes, thumbnails, transcodes, embeddings, model runtimes, temporary space, and retained source copies;
- duplicate and near-duplicate precision/recall, with approximate matches labeled;
- search latency and usefulness by modality, including freshness and generation visibility;
- open, seek, render, and first-byte latency for supported experiences;
- independent readback and clean-install restore success rate;
- operator acceptance rate for organization and metadata proposals;
- weekly return usage of Inbox, Search, and at least one domain experience;
- maintenance time, failed jobs, stale derivative burden, and upgrade/rebuild duration.

If operators still maintain separate catalogs or custom glue for ordinary tasks, the shared substrate has not delivered enough product value.

## 11. Feasibility and complexity matrix

| Experience | User demand | Shared-core reuse | Additional complexity | Main risk | Recommendation |
| --- | --- | --- | --- | --- | --- |
| Universal Inbox/catalog | High and broad | Very high | Medium | Empty-framework feeling if browse/search is weak | Make the first product wedge |
| Music player | High among NAS users | High | Low-medium | Tag inconsistencies and codec/transcode scope | Build early |
| Book/novel reader | Medium-high | High | Low-medium | Format/DRM/rendering differences | Build early after catalog |
| Video player | High | High | Medium-high | FFmpeg, codecs, seeking, CPU/GPU, subtitles | Build after music/book access works |
| Image/photo pack | High | High | Medium | Competition and privacy-sensitive AI | Integrate selectively |
| Game/app locator | Medium, distinctive | Medium-high | High | Platform profiles, secrets, DRM, changing installs | Start read-only resolver |
| Game downloader/retriever | Niche but exciting | Medium | Very high | Rights, credentials, source drift, malware, legal exposure | Defer; separate opt-in profile |
| Additional embedding spaces / CLIP semantic search | Medium-high | Medium | Medium-high | Model/storage cost, relevance, privacy, rebuild burden | Optional after the bundled local text profile |
| Magnet/P2P retrieval | Unproven for recovery | Low-medium | Very high | Persistence, trust, abuse, legal and operational risk | Defer |

## 12. Anti-monolith rules

RestoreWeave must not become a single process that embeds every player and external service. The following rules apply:

1. The core can operate headlessly with all domain packs disabled.
2. A domain pack can be removed without invalidating exact content or generic browse/restore.
3. A client can be replaced without changing storage, annotations, or collection identity.
4. A processor failure degrades understanding or preview, not exact protection.
5. Domain fields are namespaced and versioned; the baseline schema remains small.
6. Domain-specific state such as playback position or reading progress is durable annotation, not hidden UI state.
7. External retrieval and execution receive separate capabilities from parsing and indexing.
8. Convenience endpoints are compositions over typed core operations, not alternative authority paths.
9. Every profile has explicit resource, security, licensing, and recovery qualification gates.
10. No vertical is allowed to claim universal format support before a measured compatibility matrix exists.

## 13. Acceptance tests for the ecosystem shape

The design is successful when:

1. One mixed Inbox can contain unknown binaries, music, video, books, images, archives, and game files while preserving exact identity for every readable item.
2. Removing all semantic processors leaves generic search, browse, stream/read where supported, verify, and restore functional.
3. A music client and a book client can address the same server-side subject model without accessing private repository or SQLite layouts.
4. A video client can fall back from direct byte-range playback to a bounded derived transcode without changing the exact representation.
5. User tags, notes, playlists, bookmarks, ratings, and reading/playback progress survive index rebuild and client replacement.
6. An unresolved game remains searchable and restorable as generic files; resolver failure does not authorize omission.
7. A retrieval plugin cannot access credentials, network, or execution authority unless its profile explicitly grants them.
8. A user can create a minimal LinkGroup or later virtual collection without moving or duplicating physical bytes.
9. Every domain result links to an authoritative `SubjectRef` or `SegmentRef` and exposes its provenance and freshness.
10. The same group or collection can be browsed through CLI, MCP, a universal UI, and specialized clients with equivalent authorization and recovery semantics.

## 14. Product recommendation

Build the universal Inbox/catalog and exact file-shaped access as the shared foundation, then prove the ecosystem with music and books before tackling video and application retrieval. The goal is not to ship ten disconnected applications. The goal is to make one content layer useful every day, while allowing focused clients to grow around it.

RestoreWeave should remain the product name. Use product-level labels such as `RestoreWeave Catalog`, `RestoreWeave Audio`, and `RestoreWeave Books` for bundles or clients, without renaming the authoritative core.
