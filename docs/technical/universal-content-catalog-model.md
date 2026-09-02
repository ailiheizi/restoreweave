# Universal Content Catalog Model

> **Status:** Design note for the catalog and experience-pack profiles. This document makes the shared data model concrete; it does not activate media playback, semantic search, application execution, or external retrieval in `RW-MVP-1`.

## 1. Design decision

RestoreWeave uses one authoritative content and recovery plane with many bounded catalog lenses and client experiences.

```text
capture / import
  -> exact subject and namespace records
  -> optional typed artifacts and relations
  -> rebuildable per-lens projections
  -> one query broker and many clients
```

The user should be able to put an arbitrary file or directory into an Inbox, find it later, and open it in an appropriate experience when one is available. That promise does **not** require one universal parser, one giant database table, or one physical search index. It requires stable references and explicit joins between the authoritative planes.

The model has five separable planes:

| Plane | Owns | Failure or removal behavior |
| --- | --- | --- |
| **Recovery plane** | Capture, namespace, exact content, representations, placements, publication, verification, restore | Must remain usable without processors, indexes, WebUI, or AI |
| **Catalog plane** | Stable user-facing assets, observations, common metadata, tags, notes, collections, and relations | Durable records; portable and independently authorized |
| **Artifact plane** | Parser output, extracted text, thumbnails, transcripts, fingerprints, embeddings, and proposals | Versioned, attributed, rebuildable, and allowed to become stale |
| **Projection plane** | Lexical, structured, vector, acoustic, visual, graph, and recommendation indexes | Disposable generations; loss affects discovery only |
| **Experience plane** | Players, readers, photo views, game inventory, universal UI, CLI, and MCP clients | Replaceable presentation over host-mediated access |

The core owns the joins, identity, authorization, provenance, and lifecycle. Packs and clients provide interpretation and presentation within those boundaries.

## 2. Identity vocabulary

The names below are intentionally distinct. A file path, a conceptual work, a byte sequence, a preview, and a search row are not interchangeable identities.

| Type | Meaning | Typical examples |
| --- | --- | --- |
| `AssetRef` | Optional stable user-facing continuity identity across accepted renames, moves, or versions | One book, album, film, game collection, project, or unknown item |
| `SubjectRef` | Canonical typed address used by search, annotations, access, and relations; it includes a subject kind and identifier. A LinkGroup member uses the stable `file` kind, not a `FileVersionRef` or group revision. | `file`, `file_version`, `link_group`, `segment`, `artifact` |
| `NamespaceEntryRef` | One path position in one immutable snapshot namespace | `/Inbox/old.zip` in snapshot `S7` |
| `FileVersionRef` | One captured file state, including exact content and filesystem metadata | The bytes and metadata observed during capture `C19` |
| `ContentRef` | One exact logical byte stream, identified by a digest domain and length | SHA-256 plus logical length |
| `RepresentationRef` | One materialization or encoding that may satisfy a declared recovery or access contract | Raw bytes, reversible compressed bytes, validated transcode |
| `PlacementRef` | One repository or storage receipt for a representation | A Kopia object set or future object-store placement |
| `SegmentRef` | A bounded, typed portion of a subject | Audio track, video chapter, subtitle range, book page, archive member, image region |
| `CollectionRef` | A virtual, ordered or unordered set of subjects/assets | Album, playlist, reading list, watchlist, Inbox, game library, saved query |
| `ArtifactRef` | A derived, content-addressed processor result with provenance | EXIF, parse tree, extracted text, thumbnail, transcript, fingerprint, embedding |
| `RelationRef` | A typed, attributed edge between subjects/assets/segments/collections | `contains`, `member_of`, `duplicate_of`, `chapter_of`, `requires`, `similar_to` |
| `AnnotationRef` | Durable operator or client-authored state with revisions and tombstones | Tag, note, rating, bookmark, playback position, reading progress, correction |
| `CaptureRef` / `ImportBatchRef` | The bounded observation or intake operation that produced records | One watched Inbox scan or manual import |
| `IndexGenerationRef` | One immutable build of one projection provider | FTS generation 12, audio feature generation 3 |
| `ExperiencePackRef` | A signed/versioned bundle of schemas, capabilities, projections, and client metadata | Audio pack `v1`, Reader pack `v1` |

### 2.1 Asset continuity versus file version

`AssetRef` is a convenience for a user-facing concept, not proof that two
observations are the same bytes. A stable file-kind `SubjectRef` addresses the
logical file subject; immutable `FileVersionRef` and `SnapshotId` values
address its captured states. An `AssetRef` may also associate several otherwise distinct
subjects after an explicit continuity policy accepts the relationship. For
example, a book's EPUB replacement, an album's remastered track, and a game's
new install revision can share an `AssetRef` while retaining different
`ContentRef` values and recovery records. The MVP `LinkGroup` remains a
current map of stable file subjects and does not acquire a revision model from
these richer continuity mechanisms.

If continuity is uncertain, the core creates separate assets and an attributed `POSSIBLE_SAME_AS` relation. A processor or embedding distance cannot silently merge them. Exact byte identity remains solely a `ContentRef` fact.

### 2.2 Subject kinds

The `SubjectRef` union is open but host-versioned. The MVP-visible kinds needed
by the common subject model are:

```text
namespace_entry
file
file_version
link_group
segment
artifact
source_binding
```

`asset`, `virtual_member`, `collection`, and `collection_revision` remain
later-profile subject kinds. In particular, `collection_revision` MUST NOT be
used to give the MVP `LinkGroup` a revision or history model.

An index row, repository object key, filename, model output, or display URL is never a subject kind. Presentation adapters resolve those implementation details back to an authorized subject before disclosure.

## 3. Canonical relationships

The catalog is a typed graph over the identity vocabulary, not a flat media table. Every edge is versioned and attributed.

| Relation | Meaning | Example |
| --- | --- | --- |
| `OBSERVED_AS` | An asset or file version appears at a namespace entry | A renamed book remains linked to its prior path |
| `HAS_CONTENT` | A file version has exact content | File version -> `ContentRef` |
| `HAS_REPRESENTATION` | A subject can be materialized through a representation | File version -> reversible compressed form |
| `HAS_ARTIFACT` | A subject produced a derived artifact | Video -> thumbnail or transcript |
| `CONTAINS` | A parent exposes a bounded child | Archive -> virtual member; book -> chapter |
| `MEMBER_OF` | A subject belongs to a virtual collection | Track -> album or playlist |
| `DERIVED_FROM` | An artifact or representation has an input lineage | OCR text -> PDF page image |
| `REQUIRES` / `DEPENDS_ON` | Restore or use requires another component | Game save -> profile configuration |
| `SHARED_WITH` | One physical subject serves multiple logical collections | Track in two playlists |
| `DUPLICATE_OF` | Exact same content under different namespace identities | Two paths -> one `ContentRef` |
| `POSSIBLE_DUPLICATE` | Approximate or candidate match only | Acoustic or perceptual candidate |
| `SIMILAR_TO` | Discovery similarity, never identity | CLIP or melody-neighbor result |
| `SUPERSEDES` | A newer user or processor revision replaces a prior fact | Corrected metadata or new parser artifact |
| `REFERENCES` | A citation, link, or external identifier points to another subject | A note references a source document |

Relations carry the edge type and schema revision, source subjects, target subjects, producer/provenance, confidence or calibration when applicable, observation time, validity interval, and conflict state. User-confirmed relations are annotations or accepted catalog facts; processor suggestions remain proposals until accepted.

## 4. Artifact, representation, and rendition boundaries

These three concepts must not be collapsed:

1. **Representation** is a potentially durable way to recover or materialize content. It has a decoder, dependency closure, placement, and an explicit recovery claim. Exact raw and independently validated reversible encodings belong here.
2. **Artifact** is derived information used to understand or discover content. It may be deleted and rebuilt. Examples are tags extracted from a container, normalized text, OCR, captions, thumbnails, waveforms, chapters, fingerprints, and embeddings.
3. **Rendition** is an access-time view requested by a client, such as an MP3 transcode, resized image, page bitmap, subtitle format, or preview stream. A rendition may be ephemeral or cached. If retained, it is recorded as an artifact or representation with the same provenance and validation rules rather than becoming an untracked second library.

An `AccessRequest` therefore names a subject, requested fidelity, range or segment, client capability, and rendition policy. The core selects an admitted representation or invokes a qualified read-only transform. Clients receive an expiring `AccessHandle` with seek/range support, content or segment integrity evidence, and decoder requirements; they never receive repository paths or ambient host paths.

## 5. Inbox and arbitrary-content lifecycle

An Inbox is a capture/import profile plus a virtual collection, not a special exception to the data model.

### 5.1 Import records

`ImportBatchRef` records the source root or upload, actor, capture profile, arrival window, consistency class, policy revision, and source-retention decision. Each observed entry receives its own namespace, file-version, exact-content, and processing records. A batch may contain a mixture of known media, archives, executables, secrets, malformed files, and unknown binaries.

The exact lane runs before classification and remains independent of AI availability. Organization proposals may suggest a collection, rename, tag, or physical move, but no suggestion changes namespace or retention without a typed, reviewable core command.

### 5.2 Orthogonal status vector

The UI and API expose independent state dimensions instead of one overloaded `status` field:

| Dimension | Example states |
| --- | --- |
| Protection | `UNSEEN`, `EXACT_STAGED`, `EXACT_VERIFIED`, `BLOCKED`, `RETIRED` |
| Understanding | `UNKNOWN`, `IDENTIFIED`, `PARTIALLY_PARSED`, `CONFLICTING`, `UNSUPPORTED` |
| Discovery | `NOT_REQUESTED`, `QUEUED`, `INDEXED`, `STALE`, `FAILED` |
| Access | `EXACT_READABLE`, `DERIVED_AVAILABLE`, `UNAVAILABLE`, `AUTH_BLOCKED` |
| Organization | `INBOX`, `REVIEWED`, `IN_COLLECTION`, `ARCHIVED` |

For example, an unknown executable may be `EXACT_VERIFIED / UNKNOWN / INDEXED / EXACT_READABLE / INBOX`; a video may be `EXACT_VERIFIED / IDENTIFIED / STALE / EXACT_READABLE / IN_COLLECTION` while its thumbnail processor is unavailable. Neither case should be reported as “lost.”

## 6. Domain lens model

A lens is a catalog projection and interaction vocabulary over common subjects. It does not create a new source of truth.

```text
LensRef
  -> accepted content/role predicates
  -> artifact schemas and relation types
  -> query fields and facets
  -> access/rendition profiles
  -> durable state schemas
  -> optional client metadata
```

Recommended initial lenses:

| Lens | Primary subjects/segments | Typical artifacts and state |
| --- | --- | --- |
| `generic` | Any namespace entry or file version | Type evidence, text, hashes, tags, notes |
| `audio` | Track, album, disc, playlist | Tags, duration, waveform, acoustic fingerprint, queue and position |
| `video` | Film/episode/file, stream, chapter, subtitle/time range | Technical metadata, keyframes, subtitles, transcript, watch progress |
| `reader` | Book/document, chapter, page, text range | TOC, normalized text, OCR, bookmark, highlight, reading progress |
| `image` | Image/photo, region, album/event | EXIF, thumbnail, OCR/caption, visual fingerprint, rating |
| `application` / `game` | Collection, component, save/mod/config | Manifest, dependency graph, platform facts, restore checklist |
| `archive` / `code` / `dataset` / `model` | Container/member or project/schema | Member inventory, symbols, schema, model metadata, license notes |

The generic lens is mandatory. A specialized lens is optional and can be removed without changing exact protection.

## 7. Experience-pack contract

An `ExperiencePack` is a signed, versioned capability bundle. It may contain:

- namespaced artifact and relation schemas;
- `Processor` capability profiles for parse, extract, fingerprint, transform, validate, or index preparation;
- optional `IndexProvider` projections and `QueryProvider` presets;
- access/rendition profiles and client-declared codec/rendering requirements;
- durable state schemas and migration rules for playlists, bookmarks, progress, or ratings;
- presentation metadata and conformance fixtures;
- dependency, license, privacy, egress, and resource declarations.

The host validates the manifest, grants only declared capabilities, and builds new artifact/index generations beside active ones. A pack cannot write authoritative catalog, namespace, policy, repository, or publication records directly.

Pack removal or failure has predictable semantics:

- exact representations, namespace browse, generic search, verification, and restore remain available;
- durable user annotations, collections, and progress remain exportable;
- pack-produced artifacts may remain visible as stale records or be purged by policy;
- pack-dependent projections are marked unavailable or stale;
- a representation cannot be retired while a recovery contract still depends on its decoder or pack dependency;
- reinstallation may rebuild missing artifacts and projections from authoritative subjects.

## 8. One search experience, many physical projections

“Search everything” is a user-facing contract, not a demand for one physical index. The catalog maintains a small common metadata projection plus optional lens-specific generations.

```text
user query + lens hint + structured filters
  -> QueryBroker creates bounded subqueries
  -> each QueryProvider receives exactly one named IndexGenerationRef
  -> providers return SubjectRef / SegmentRef candidates
  -> broker fuses, reauthorizes, labels approximation and staleness
  -> client renders one result set with explainable provenance
```

Possible projections include baseline FTS, audio/acoustic, video/visual, reader/full-text, image/vector, and graph or recommendation indexes. Different models, tokenizers, dimensions, codecs, or ranking rules create separate generations. No index row ID or vector ID becomes a durable identity. Losing a lens projection degrades that lens only; the generic projection and exact access remain.

## 9. Product examples

### 9.1 Music

An audio file is exact-protected as a `FileVersionRef` and may expose a track `SegmentRef`, an album `CollectionRef`, metadata and waveform `ArtifactRef`s, and a playback-position `AnnotationRef`. A melody or acoustic algorithm can emit `POSSIBLE_DUPLICATE` or `SIMILAR_TO` evidence. It cannot claim that two recordings are byte-identical or replace the master.

### 9.2 Video

A video file remains the exact subject. Streams, chapters, subtitles, keyframes, and time ranges are segments or artifacts. A player requests an `AccessHandle`; the core may serve the original or a validated rendition according to client capabilities. Transcode caches are disposable and never alter restore truth.

### 9.3 Books and novels

An EPUB/PDF/CBZ remains the protected source. Chapters, pages, and text ranges become segments; extracted text and OCR become artifacts; bookmarks, highlights, notes, and reading progress become durable annotations. A renderer can be replaced without changing the source identity or user state.

### 9.4 Applications and games

A detected game is an `AssetRef` or collection revision containing exact component subjects for payload, saves, configuration, mods, DLC, runtimes, and manifests. Dependency and source-binding relations explain what can be restored exactly, reacquired, or requires manual action. A resolver cannot execute, download, delete, or weaken protection.

### 9.5 Unsorted material

An unknown file needs no special plugin to be useful. It has an exact `ContentRef`, original namespace entry, type evidence, generic metadata, duplicate relations, processing state, and an Inbox membership. Later processors can add meaning without changing the original record.

## 10. Delivery gates

Implement in this order:

1. **Core exact slice:** capture, exact identity, repository placement, publication, namespace read, verify, and clean restore.
2. **Universal Catalog:** ImportBatch/Inbox, generic metadata, baseline lexical search, durable annotations, virtual collections, and one item page.
3. **Access contract:** seek/range/stream handles, rendition negotiation, and a small universal client.
4. **First lens:** music or reader, selected by available parser/decoder and operator demand; keep the first client read-only.
5. **Additional lenses:** video, image, document, archive, code, dataset, and model views as processors and fixtures mature.
6. **Application/game resolver:** read-only collection/component graph and restore planning.
7. **Semantic and retrieval profiles:** embeddings/CLIP, external metadata, store/package connectors, and P2P only after lexical value, privacy, rights, and recovery gates pass.

Do not implement all experiences in the core process before the first vertical slice is useful. Build the shared contracts once, then add one measurable daily workflow at a time.

## 11. Acceptance criteria

1. An arbitrary mixed Inbox item is exact-protected and generically searchable even when every specialized processor is absent.
2. One subject can be addressed by generic search and at least two domain lenses without duplicate source-of-truth records.
3. Domain artifacts, relations, and projections include producer, configuration, input revision, coverage, and freshness.
4. User annotations and domain state survive path changes, index rebuilds, pack replacement, and client replacement according to their declared subject scope.
5. A player or reader obtains only a scoped access/rendition handle and never a repository-private path.
6. Approximate fingerprints and embeddings produce labeled candidates, never exact identity or recovery claims.
7. Removing an experience pack leaves exact browse, verify, restore, generic search, and portable user data operational.
8. A game/application resolver can explain component and dependency state but cannot execute or retrieve content without a separately qualified profile.
9. A unified query returns stable subjects or segments and identifies provider generations, stale fields, and approximate matches.
10. A clean installation can restore exact content without any experience pack, index, model, or UI.
