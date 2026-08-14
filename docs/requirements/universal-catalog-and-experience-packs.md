# Universal Catalog and Experience Pack Overview

> **Document status:** Non-normative orientation guide. The authoritative requirements are in [Ecosystem and Vertical Application Requirements](ecosystem-and-vertical-apps.md). If this overview and the normative document differ, the normative document wins. Nothing in this overview expands `RW-MVP-1` or implies that the later experiences are already implemented.

## What this means

RestoreWeave is one content and recovery data plane with many bounded ways to use it:

```text
RestoreWeave Core
  -> Universal Catalog and Inbox
  -> Music, Books, Video, Photos, Documents, Games, and other experience packs
  -> CLI, MCP, universal UI, FUSE, and third-party clients
```

The core keeps exact identity, the file-shaped namespace, representations, provenance, verification, authorization, and restore semantics. Experience packs interpret content and provide focused views such as a music library, a novel reader, a video browser, a photo workspace, or an application/game inventory. A pack or client can be removed without making exact content unsearchable or unrestorable.

The product is not a single binary that embeds every parser, codec, model, player, downloader, and launcher. It is also not an empty plugin host that forces operators to assemble a usable system. The reference distribution should provide a useful universal catalog first, with replaceable processors and clients around a small authoritative kernel.

## The user journey

```text
drop almost anything into an Inbox or attach an existing tree
  -> capture and hash exact bytes
  -> identify by suffix, magic bytes, and bounded structural evidence
  -> expose a generic item immediately
  -> enrich asynchronously when processors are available
  -> search, tag, annotate, and place into virtual collections
  -> open it in the best available domain experience
  -> verify and restore the original path and bytes with proof
```

Unknown or unsupported content remains exact, searchable, inspectable, and restorable. AI may suggest classification, metadata, tags, summaries, or semantic matches, but it does not decide to omit, delete, weaken protection, or claim exact recovery.

## Why the substrate is shared

Music, video, books, images, games, archives, and miscellaneous files have different metadata but common durable needs:

- a stable subject and namespace location;
- exact content identity independent of path or placement;
- representations and artifacts with provenance and fidelity claims;
- bounded reads, streams, previews, and restore operations;
- user-authored tags, notes, collections, ratings, and progress;
- search results that remain authorized and traceable after index rebuilds.

The common catalog uses small host-owned objects such as `SubjectRef`, `ContentRef` (`ContentIdentity` in product language), `RepresentationRef`, `SegmentRef`, `CollectionRef`, `AnnotationRef`, `ArtifactRef`, `SnapshotId`, and `AccessHandle`. Domain fields stay namespaced (for example `audio.artist`, `book.author`, or `video.duration`) instead of expanding one universal table.

The durability boundary is:

| Layer | Purpose | Recovery meaning |
| --- | --- | --- |
| Core subject facts | Paths, snapshots, exact digests, representations, verification | Authoritative |
| Common catalog facts | Tags, notes, collections, processing state | Durable user/catalog data |
| Domain artifacts | Chapters, stream metadata, EXIF, transcripts, fingerprints | Versioned and rebuildable unless promoted |
| Search projections | Lexical, vector, visual, acoustic, or graph indexes | Rebuildable and generation-pinned |

“One search” is a unified query experience, not a requirement for one physical mega-index. The query broker may federate several generation-pinned projections and return stable subjects or segments with provenance, freshness, approximation, and availability information.

## Experience examples

- **Music:** metadata, albums/tracks, playlists, range-aware playback, exact duplicates, and optional acoustic or melody similarity. The user's `decentralized-music` project can be an optional processor; similarity evidence is never exact identity.
- **Books and novels:** EPUB/PDF/CBZ and other qualified formats, chapters/pages, full text, reading progress, bookmarks, highlights, and notes.
- **Video:** stream metadata, subtitles, chapters, thumbnails, time segments, and bounded original playback or transcode.
- **Photos and documents:** EXIF/XMP, OCR, text extraction, previews, duplicate candidates, and privacy-controlled semantic artifacts.
- **Applications and games:** locate and explain roots, manifests, saves, mods, DLC, runtimes, and dependencies before any restore planner or external retrieval.
- **Everything else:** a generic item page, path/type/hash search, annotations, safe open/export, and exact restore.

These are views and clients over the same content plane, not separate libraries that silently copy or redefine data.

## Delivery order at a glance

1. `RW-CORE-1`: exact ingest, repository qualification, publication, verify, namespace browse, and restore.
2. `RW-CATALOG-1`: universal Inbox, generic item page, lexical search, annotations, and virtual collections.
3. `RW-AUDIO-1` and `RW-BOOK-1`: small read-only music and reader experiences.
4. `RW-VIDEO-1`, image/document processors, and selected semantic projections.
5. `RW-COLLECTION-1`: read-only application/game resolution and restore planning.
6. `RW-RETRIEVE-1` and richer semantic or multimodal profiles, always opt-in and separately qualified.

The first compelling demonstration is a mixed Inbox followed by a small music slice. It proves that RestoreWeave can become a daily discovery tool without waiting for every media domain or AI model.

## Read next

- [Ecosystem and Vertical Application Requirements](ecosystem-and-vertical-apps.md) — normative product requirements, scope, profiles, release sequence, metrics, risks, and acceptance tests.
- [Universal Content Catalog Model](../technical/universal-content-catalog-model.md) — identity graph, artifacts, projections, and query federation.
- [Ecosystem App Interface](../technical/ecosystem-app-interface.md) — domain records, segments, collections, renditions, access handles, manifests, and app grants.
- [Driver and Processor Interfaces](driver-and-processor-interfaces.md) — replaceable capture, processing, storage, indexing, and query seams.
- [CLI and MCP Contract](cli-and-mcp-contract.md) — the preferred automation and AI integration surface.
- [Ecosystem Application and Adapter References](../references/ecosystem-application-adapters.md) — verified open-source projects that can be integrated or used as reference clients.

RestoreWeave remains the product name. Product-level bundles or clients may use labels such as `RestoreWeave Catalog`, `RestoreWeave Audio`, and `RestoreWeave Books`; they do not create new storage authorities.
