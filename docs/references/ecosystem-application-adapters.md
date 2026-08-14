# Ecosystem Application and Adapter References

> **Research status:** Design references verified against the listed project repositories on 2026-08-12. These projects are not automatically adopted dependencies. Their licenses, APIs, runtime assumptions, and security boundaries must be requalified before code reuse or distribution.

## 1. Decision

RestoreWeave should be a shared content and recovery substrate with focused domain lenses, clients, and adapters. It should not become one monolithic process that contains a music server, video transcoder, ebook reader, game launcher, downloader, and general-purpose workflow engine.

The practical product shape is:

```text
RestoreWeave Core
  -> Universal Inbox and Catalog
  -> Domain processors and typed artifacts
  -> Music / Video / Reader / Photo / Document / Game lenses
  -> CLI, MCP, REST, WebUI, and native-client adapters
```

Every lens refers back to the same `SubjectRef`, exact `ContentIdentity`, `SnapshotTree`, `SegmentRef`, `Annotation`, and scoped `FileAccess` handle. A lens may disappear or be replaced without changing exact storage, namespace meaning, or restore records.

Whole-pipeline introduction order and retrofit traps are in [Whole-Architecture Open-Source Reference](../technical/architecture-open-source-reference.md).

## 2. Verified candidate map

| Area | Project and primary source | Mechanism worth borrowing | Recommended RestoreWeave boundary | License evidence |
| --- | --- | --- | --- | --- |
| Music | [Navidrome](https://github.com/navidrome/navidrome) | Library scanning, metadata-driven albums/tracks, playlists and play history, change monitoring, on-the-fly transcoding, and the Subsonic-compatible API | External music client or protocol adapter. It should consume scoped streams and catalog results; it must not read repository packs or RestoreWeave's private catalog tables. | [GPL-3.0](https://github.com/navidrome/navidrome/blob/master/LICENSE); keep as a separate process unless the distribution accepts the copyleft boundary. |
| Music metadata | [beets](https://github.com/beetbox/beets) | MusicBrainz-oriented tagger, import workflow, and a small plugin model for metadata enrichment | Isolated `Processor.PARSE`/`ENRICH` candidate or sidecar tool. Proposed tag changes remain reviewable; source files are not silently rewritten. | [MIT](https://github.com/beetbox/beets/blob/master/LICENSE); verify plugin and dependency licenses separately. |
| Music metadata | [MusicBrainz Picard](https://github.com/metabrainz/picard) | AcoustID/MusicBrainz matching and human-reviewed tag editing | UX and matching reference; optional subprocess processor, not recovery authority. | [GPL-2.0-or-later](https://github.com/metabrainz/picard/blob/master/COPYING.txt); retain process boundary for a permissive core. |
| Audio similarity | [Chromaprint](https://github.com/acoustid/chromaprint) | Compact acoustic fingerprints for near-match candidate discovery | Optional `Processor.FINGERPRINT`; similarity results are approximate evidence and never exact identity. | [License note](https://github.com/acoustid/chromaprint/blob/master/LICENSE.md) says the aggregate is LGPL-2.1 because bundled FFmpeg code is included. |
| Video | [Jellyfin](https://github.com/jellyfin/jellyfin) | Media-library model, metadata refresh, playback API, stream/range behavior, subtitle and transcoding workflows | Separate media-server integration or client reference. A RestoreWeave video lens should initially expose metadata, original reads, and a few qualified renditions rather than embed a complete media server. | [GPL-2.0](https://github.com/jellyfin/jellyfin/blob/master/LICENSE); inspect the server, web client, codecs, and bundled dependencies independently. |
| Books/comics | [Komga](https://github.com/gotson/komga) | Library/series/book hierarchy, REST API, OPDS, reading lists, progress, metadata editing, and reader sync | Strong adapter and client contract reference. A Komga bridge can map books and pages to `SubjectRef`/`SegmentRef` and use scoped `FileAccess`. | [MIT](https://github.com/gotson/komga/blob/master/LICENSE); direct code reuse still requires dependency review. |
| Books/comics | [Kavita](https://github.com/Kareadita/Kavita) | Cross-format reading server, library organization, reader UX, and progress tracking | Separate reader or integration reference; do not duplicate its catalog as a second source of truth. | [GPL-3.0](https://github.com/Kareadita/Kavita/blob/master/LICENSE). |
| E-books | [calibre](https://github.com/kovidgoyal/calibre) | Broad format metadata, cataloging, conversion, device-oriented workflows, and external metadata lookup | Isolated reader/metadata/conversion processor. Conversion output is a derived representation until independently validated; no DRM bypass. | [GPL-3.0](https://github.com/kovidgoyal/calibre/blob/master/LICENSE). |
| Audiobooks | [Audiobookshelf](https://github.com/advplyr/audiobookshelf) | Chapter-aware audiobook/podcast catalog, playback position, and client synchronization | Optional audiobook lens over audio subjects and time segments; app-owned progress is durable annotation, not storage truth. | [GPL-3.0](https://github.com/advplyr/audiobookshelf/blob/master/LICENSE). |
| Games/ROMs | [RomM](https://github.com/rommapp/romm) | Scan, metadata enrichment, platform normalization, tags, artwork, and browser/emulator playback | Later read-only game-collection resolver and client adapter. External metadata and emulator launch require explicit profiles; never grant execution or deletion authority by default. | [AGPL-3.0](https://github.com/rommapp/romm/blob/master/LICENSE). |
| Games | [Lutris](https://github.com/lutris/lutris) | Launcher/platform metadata and game installation/runtime concepts | Design reference for a future `CollectionResolution` profile. Do not embed launcher execution in Core. | [GPL-3.0](https://github.com/lutris/lutris/blob/master/LICENSE). |
| Games | [Heroic Games Launcher](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher) | Provider-specific game manifests and launcher integration for Epic/GOG/Amazon | Provider adapter reference only. Download, credentials, entitlement, and execution belong to a later `RetrieverDriver`/validation profile. | [GPL-3.0](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/blob/main/COPYING). |
| Games | [Pegasus Frontend](https://github.com/mmatyas/pegasus-frontend) | Game-library browsing and launching from generated metadata without owning the underlying storage | Prefer a metadata-export adapter over building a launcher into RestoreWeave. Execution remains client-owned and separately authorized. | [GPL-3.0-or-later](https://github.com/mmatyas/pegasus-frontend/blob/master/LICENSE.md). |
| Reader client | [KOReader](https://github.com/koreader/koreader) | Broad local ebook/document format support and a replaceable device-oriented reader | Consume file-shaped access or an OPDS facade. If progress/highlight synchronization is added, normalize it to portable subject/segment annotations. | [AGPL-3.0](https://github.com/koreader/koreader/blob/master/COPYING). |
| Mixed filesystem search | [sist2](https://github.com/sist2app/sist2) | Incremental scans, archive recursion, metadata/text extraction, OCR, thumbnails, tagging, and SQLite/Elasticsearch/embedding search | Strong external index/processor reference and possible isolated sidecar. Do not let its index become recovery authority or duplicate RestoreWeave namespace identity. | [GPL-3.0](https://github.com/sist2app/sist2/blob/master/LICENSE). |
| Offline tagging | [TagSpaces](https://github.com/tagspaces/tagspaces) | Local-first tagging, folder browsing, and sidecar/filename metadata patterns | UX reference for Inbox triage and portable annotations; do not make filename tags the authoritative identity model. | Repository metadata reports [AGPL-3.0](https://github.com/tagspaces/tagspaces); review subpackages and extensions separately. |
| Document archive | [Paperless-ngx](https://github.com/paperless-ngx/paperless-ngx) | OCR, full-text search, tags, custom fields, workflows, and reviewed metadata suggestions | Document experience pack or isolated extraction/index adapter. Preserve original exact documents and expose suggestions as attributed artifacts. | [GPL-3.0](https://github.com/paperless-ngx/paperless-ngx/blob/main/LICENSE). |
| Tag-centric media | [Hydrus Network](https://github.com/hydrusnetwork/hydrus) | Large personal media collections organized by tags, ratings, and relationships, with embedded playback | Product/design reference for high-volume tag workflows. Do not reuse code until the applicable license is verified; its model is not a recovery namespace. | License status is not represented by a reliable SPDX value in repository metadata; treat as unqualified. |
| Content-addressed storage | [Perkeep](https://github.com/perkeep/perkeep) | Content-addressed blobs, claims, imports, mutable views, and provenance-oriented modeling | Selective code/design borrowing candidate for content/range-read and conformance-test patterns; keep RestoreWeave identity and publication semantics authoritative. | [Apache-2.0](https://github.com/perkeep/perkeep/blob/master/COPYING). |

## 3. What belongs in an adapter

An experience pack or external application may own:

- domain parsers and namespaced metadata (`audio.artist`, `book.chapter`, `video.subtitle`);
- thumbnails, waveforms, transcripts, previews, and other rebuildable artifacts;
- domain query presets, facets, ranking, and recommendation presentation;
- player/reader controls, queues, playlists, bookmarks, ratings, and progress;
- external metadata providers and user-facing enrichment workflows;
- game/app collection resolution and, in a separately qualified profile, retrieval or validation.

It must use host-brokered operations for identity, authorization, content reads, annotations, and jobs. It must never construct a host path from a display path, inspect repository-private objects, write SQLite tables directly, or treat a model match, metadata match, torrent, or launcher receipt as exact recovery proof.

## 4. What belongs in the Core

The Core should remain responsible for:

- exact content identity, namespace and snapshot lineage;
- representation admission, publication, verification, restore, and garbage-collection eligibility;
- bounded `FileAccess` and seek/range-capable streams;
- stable `SubjectRef` and `SegmentRef` references;
- durable user annotations and portable export;
- catalog authorization, query-generation lifecycle, and result reauthorization;
- processor/repository/index capability negotiation and provenance;
- quarantine and policy boundaries for external retrieval.

This split lets one server power a music player, video player, reader, universal Inbox, and later game catalog without forcing every feature into one binary or creating multiple conflicting libraries.

## 5. Integration modes and license policy

Use the least-coupled mode that preserves the product boundary:

1. **Protocol/client adapter:** consume a stable REST, OPDS, Subsonic, or typed RestoreWeave API from a separate process. Preferred for GPL/AGPL applications.
2. **Isolated processor:** invoke a pinned CLI or worker through the `Processor` contract with bounded handles and no ambient authority.
3. **Selective library dependency:** permitted primarily for MIT/BSD/Apache/LGPL components after license, notice, security, and reproducibility review.
4. **Design reference only:** use UX and data-model lessons without linking or copying code when licenses, runtime scope, or security assumptions do not fit.

GPL/AGPL projects are not automatically unusable, but linking them into a permissively licensed RestoreWeave Core creates distribution and source-obligation questions. Keep them as independent services or optional binaries unless the project deliberately adopts compatible licensing. FFmpeg, ExifTool, Tika, and other multi-license stacks require component-level SBOM and notice review; a project README license claim is not sufficient.

The first protocol facades worth testing are [OpenSubsonic](https://opensubsonic.netlify.app/) for music and [OPDS](https://opds.io/) for books and comics. They are ecosystem compatibility adapters, not canonical RestoreWeave authority APIs. A Pegasus-compatible metadata export is a similarly narrow bridge for game frontends. Jellyfin-style media APIs should wait until direct-play, seek, subtitle, transcode, authorization, and cache behavior have been qualified. Audiobookshelf is useful as an audiobook UX and state-model reference, but its current API documentation warns that it is out of date; do not freeze an adapter against it before contract verification.

## 6. Recommended ecosystem sequence

1. **Universal Inbox/Catalog:** mixed-file exact ingest, lexical search, annotations, virtual collections, item page, and scoped open/download.
2. **Music lens:** metadata, duplicate/fingerprint candidates, playlists, and local streaming. Use `decentralized-music` as an optional similarity processor.
3. **Reader lens:** EPUB/PDF/CBZ structure, full text, progress, bookmarks, and notes.
4. **Video and photo lenses:** metadata, thumbnails, subtitles, and bounded playback/renditions.
5. **Application/game resolver:** read-only collection/component/dependency evidence and restore planning.
6. **Retrieval/downloaders:** provider-bound, quarantined, human-approved, and independently validated; P2P/magnets remain optional later profiles.

The first compelling demo is not “every player.” It is: drop a mixed directory into the Inbox, find any item through one catalog, open it in the appropriate lens, annotate it, and restore the original path and bytes after removing all indexes and optional processors.
