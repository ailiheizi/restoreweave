# Ecosystem Application Interface and Content Access Contract

> **Profile status:** This is a later northbound/catalog contract above `RW-MVP-1`. It defines how domain packs and focused clients can share RestoreWeave's identity, access, search, annotation, and recovery semantics. It does not make a media player, book reader, launcher, downloader, WebUI, or AI service part of the authoritative storage kernel.

## 1. Purpose and product boundary

RestoreWeave can support a family of useful applications over one managed data plane: a universal Inbox, music player, video player, book or novel reader, photo browser, document workspace, application/game inventory, and later approved retrieval clients. The correct shape is **one data plane, several bounded experiences**:

```text
RestoreWeave Core
  -> universal catalog and Inbox
  -> domain packs (audio, video, books, images, documents, games, ...)
  -> clients (catalog UI, players, readers, CLI, MCP, export consumers, ...)
```

The core owns durable identity, exact bytes, namespace, provenance, annotations, access authorization, publication, verification, and restore meaning. Domain packs provide schemas and bounded processors. Clients provide presentation and ephemeral interaction state. A client MUST NOT read SQLite tables, repository packs, object locators, or source paths directly.

This contract intentionally does not add a new storage engine seam. A domain pack uses the existing `Processor`, `IndexProvider`, `QueryProvider`, and later `RetrieverDriver` contracts. A client is an adapter over the Core Command ABI and the stable `SnapshotTree`/`FileAccess` read contracts.

## 2. Design rules

1. **One identity graph.** Every domain result resolves to an existing `SubjectRef`, `ContentRef`, `RepresentationRef`, `SnapshotId`, or `SegmentRef`.
2. **Generic fallback.** Missing or failed domain processing leaves exact generic browse, search, read, verify, and restore available.
3. **No hidden copies.** Players and readers may cache bytes or derivatives, but caches are scoped and rebuildable; they do not create a second catalog or recovery authority.
4. **Exact by default.** A direct open or download means the accepted exact representation unless an explicit non-exact contract is selected.
5. **Domain data is namespaced.** Audio, book, game, and other fields use versioned schemas and cannot silently redefine core identity.
6. **Sessions are not publication.** Playback queues, current page, decoder state, and UI layout are app/session state. Durable user intent uses the catalog annotation and collection records.
7. **Discovery is not acquisition.** Locating a source, presenting a candidate, acquiring bytes, and authorizing execution are separate operations and capability grants.
8. **Human authority remains explicit.** A recommendation, client action, model result, or download button cannot authorize omission, deletion, fidelity reduction, or execution.

## 3. Shared object model

### 3.1 Host-owned references

The following references are stable across clients and domain-pack upgrades:

| Reference | Meaning | Authority |
| --- | --- | --- |
| `SubjectRef` | A source entry, directory, file version, collection, domain entity, representation, or other catalog subject | Core |
| `ContentRef` | Exact byte identity (digest plus logical length). `ContentIdentity` is the product-language alias. | Core |
| `RepresentationRef` | One exact, reversible, derived, or approximate form and its provenance | Core |
| `SnapshotId` | One immutable namespace and recovery view | Core |
| `PathRef` | One component-resolved entry in a snapshot namespace | Core |
| `SegmentRef` | One immutable, bounded part of a subject, such as a track, chapter, page, subtitle span, or time range | Core envelope; domain coordinates |
| `DomainRecordRef` | One versioned domain interpretation attached to one or more subjects | Domain profile, authenticated by core |
| `CollectionRef` | One virtual ordered set or saved query over subjects or segments | Catalog profile, authenticated by core |
| `AccessHandle` | A principal-bound, expiring byte or stream capability | Core |
| `ArtifactRef` | A rebuildable extraction, preview, fingerprint, transcript, or embedding | Core provenance; domain schema |

`SubjectRef` remains the join key. A domain pack MUST attach records to stable subjects rather than replacing them with path strings or provider-private IDs.

### 3.2 Domain records

A domain record is an extension-owned interpretation, not a new source of byte truth. The host stores or authenticates a bounded `DomainRecordEnvelope/v1`:

~~~json
{
  "record_ref": "domain-record:01K2...",
  "schema_ref": "org.restoreweave.domain.audio.track/v1",
  "domain_pack_ref": "domain-pack:audio@1",
  "revision": 3,
  "subject_refs": ["subject:01K2..."],
  "parent_record_refs": ["domain-record:audio.album:..."],
  "segment_ref": "segment:01K2...",
  "fields": {
    "title": "Example track",
    "artist": "Example artist",
    "duration_ms": 214000,
    "codec": "flac"
  },
  "artifact_refs": ["artifact:audio-metadata:..."],
  "relations": [
    {"kind": "PART_OF", "target_ref": "domain-record:audio.album:..."}
  ],
  "coverage": {"state": "COMPLETE", "inspected_ranges": []},
  "provenance_ref": "provenance:...",
  "acl_ref": "acl:...",
  "state": "CURRENT"
}
~~~

The host validates schema identity, size, subject authorization, input lineage, producer digest, coverage, ACL labels, and revision rules. `fields` are namespaced and schema-checked; arbitrary SQL columns or unbounded JSON do not become a compatibility surface. A failed or stale domain record is visible as degraded metadata and never invalidates the underlying subject.

### 3.3 Segments and domain entities

`SegmentRef` is immutable and content-bound. Its canonical record includes:

- Parent `SubjectRef` or `DomainRecordRef`.
- Segment schema and coordinate unit (`BYTE`, `NANOSECOND`, `FRAME`, `PAGE`, `CHARACTER`, or `ITEM`).
- Canonical start and end values (integer or rational; floating-point time is not canonical).
- Optional ordinal, label, language, and child/parent relations.
- The parser or index artifact digest that established the boundary.
- Revision, coverage, and ACL state.

For a time segment, the segment is a logical selection, not proof that the bytes occupy one contiguous range. A qualified container index or transcode processor may map it to byte ranges or a derived representation. Segment identity does not replace `ContentRef`.

`DomainRecordRef` can represent a work, edition, part, installation, profile, or provider interpretation across several files. For example, an album may point to multiple track subjects and a book may link an EPUB subject to chapter segments and cover-art subjects. Overlapping interpretations are allowed; membership never grants ownership or deletion authority.

### 3.4 Representations and renditions

RestoreWeave MUST retain `RepresentationRef` as the only canonical representation identity. `RepresentationId` is the identifier carried by that reference. Client terminology such as “rendition,” “proxy,” “stream,” “preview,” or “download” is a delivery purpose over a representation, not another identity class.

An app-facing `RepresentationCandidate` may include:

- Representation and parent subject references.
- Kind and recovery claim (`EXACT_RAW`, `EXACT_REVERSIBLE`, `DERIVED`, `APPROXIMATE`).
- Purpose (`ORIGINAL`, `PLAYBACK`, `READING`, `PREVIEW`, `THUMBNAIL`, `DOWNLOAD`, `RESTORE`).
- Media type, codec, dimensions, sample rate, language, bitrate, and duration where applicable.
- Direct/range/seekability capabilities and required decoder or transform profile.
- Placement, verification, freshness, and estimated cost.

An unspecified or `ORIGINAL` selection resolves to the authoritative exact representation. A client may request a `PLAYBACK` or `PREVIEW` candidate, but the broker MUST report when it is derived or approximate and MUST never substitute it for an exact restore/read request.

## 4. Virtual collections and durable user state

Collections are virtual by default and do not move or duplicate physical bytes. A later catalog profile should provide two forms:

- **Static collection:** an ordered, explicitly edited membership list of `SubjectRef` or `SegmentRef` values.
- **Dynamic collection:** a saved, schema-checked query plus an optional pinned index-generation policy. Membership is evaluated against an authorized generation and is labeled as dynamic.

A `CollectionRevision/v1` contains:

~~~json
{
  "collection_ref": "collection:01K2...",
  "revision": 7,
  "owner_ref": "principal:local-user",
  "kind": "PLAYLIST",
  "title": "Study music",
  "description": "",
  "membership": [
    {"member_ref": "segment:track-1", "position": "000001", "added_at": "2026-08-12T03:00:00Z"},
    {"member_ref": "subject:book-2", "position": "000002", "added_at": "2026-08-12T03:01:00Z"}
  ],
  "query_ref": null,
  "visibility": "PRIVATE",
  "predecessor_revision": 6,
  "provenance_ref": "provenance:..."
}
~~~

Membership updates use immutable successor revisions and optimistic concurrency. A collection may contain mixed domains; a client decides whether a member is playable/readable. Missing or unauthorized members remain represented with typed status rather than silently disappearing from a durable collection.

Playback position, reading progress, bookmarks, highlights, ratings, watch history, queue state, and review decisions are typed annotation records. The host owns revision, subject binding, visibility, export, tombstone, and authorization; the domain pack owns the payload schema. Ephemeral decoder state, open queues, and UI layout remain app-owned and may be discarded.

## 5. App-facing read and stream contract

The existing `SnapshotTree` and `FileAccess` contracts are the only byte authority. A domain client composes them through a bounded app service:

~~~text
resolve subject/domain record
-> select authorized representation under an access intent
-> optionally resolve SegmentRef and byte/seek map
-> issue principal-bound AccessHandle
-> read bounded ranges or stream chunks
-> report covered bytes, decoder state, and verification status
~~~

Illustrative transport-neutral shapes:

~~~go
type ContentAppAccess interface {
    Describe(ctx context.Context, subject SubjectRef, profile DomainProfileRef) (DomainView, error)
    ListSegments(ctx context.Context, subject SubjectRef, schema SchemaRef, page PageToken) (SegmentPage, error)
    Open(ctx context.Context, req OpenContentRequest) (AccessHandle, error)
    Read(ctx context.Context, handle AccessHandle, offset uint64, length uint32) (Chunk, error)
    Close(ctx context.Context, handle AccessHandle) error
}
~~~

The Go form is illustrative. The stable semantics are:

`OpenContentRequest` includes workspace/principal, subject or segment, snapshot, access intent (`BROWSE`, `PLAYBACK`, `READING`, `PREVIEW`, `DOWNLOAD`, or `RESTORE`), representation selector, target format constraints, optional time/page/byte range, expiry, and byte/compute budgets. The result includes handle, selected representation, media type, logical length, seekability, content digest when known, segment mapping, expiry, and current verification state. `OpenMediaRequest` may remain a transport-level compatibility alias for media-only adapters, but it is not a core vocabulary term.

Direct playback/read uses the original or an already admitted derived representation. If a client needs a transcode or normalized rendition, the host invokes a pinned `Processor.TRANSFORM` capability and stages a rebuildable representation before issuing a handle. On-demand transform is bounded, cacheable, and auditable; arbitrary ffmpeg, shell, or URL execution is not an app API.

`RangeMapArtifact/v1` may map logical time/page/segment coordinates to byte ranges or decoder checkpoints. It is a derivative with parser provenance, not a replacement for the source content identity. A range read proves only the covered range unless the host performs a full verification.

HTTP `GET`, `HEAD`, `Range`, content-disposition, and media-server protocols are presentation adapters over this contract. They must bind cache keys and responses to the exact snapshot, subject, representation, range, principal, and verification context.

## 6. Domain-pack and client manifests

### 6.1 Domain-pack manifest

A signed domain pack declares schemas and processing capabilities, not storage authority:

~~~json
{
  "schema": "org.restoreweave.domain-pack-manifest.v1",
  "pack_id": "org.restoreweave.audio",
  "version": "1.0.0",
  "package_digest": "sha256:...",
  "publisher": "...",
  "schemas": [
    "org.restoreweave.domain.audio.album/v1",
    "org.restoreweave.domain.audio.track/v1"
  ],
  "segment_schemas": ["org.restoreweave.segment.audio.track/v1"],
  "processor_profile_refs": ["processor-profile:audio-parse@1"],
  "index_fields": ["audio.artist", "audio.album", "audio.duration_ms"],
  "representation_uses": ["PLAYBACK", "PREVIEW"],
  "supported_media_types": ["audio/flac", "audio/mpeg", "audio/ogg"],
  "permissions": {"network": false, "secrets": false, "execution": false},
  "dependencies": [],
  "license_refs": ["license:..."],
  "compatibility": {"core_abi": ">=1 <2"}
}
~~~

The manifest must also declare deterministic behavior, streaming/seek support, resource limits, decoder/runtime dependencies, privacy and egress requirements, migration/rebuild behavior, and removal rules. Unknown critical fields fail closed. A pack may be removed without invalidating exact content or generic catalog records.

### 6.2 Client/application manifest

A player, reader, catalog UI, or inspector may register a client manifest containing:

- App ID, version, package digest, publisher/signature, and supported core ABI.
- Accepted domain schema and media-type ranges.
- Supported intents (`OPEN`, `PLAYBACK`, `READ`, `INSPECT`, `QUEUE`, `ANNOTATE`).
- Required fields/artifacts and requested access scopes.
- Deep-link or local launch target; no ambient source paths.
- Session-state retention and export behavior.
- Network, secret, execution, and external-egress declarations.

The core advertises compatible clients through capability discovery. Launching a client is a presentation decision and never implies execution permission for recovered applications or games.

## 7. Search and catalog integration

Domain packs contribute namespaced fields, segment records, facets, and `INDEX_PREPARE` artifacts to the normal replayable index feed. They do not create independent authoritative databases. Queries use the typed filter tree and one explicit `IndexGenerationRef`; a broker may fuse lexical and domain generations only when each component remains generation-pinned and its score semantics are disclosed.

Domain results should include:

- `subject_ref`, optional `domain_record_ref`, and optional `segment_ref`.
- Matched field or artifact reference and score semantics.
- Snapshot/index generation, producer digest, freshness, coverage, and authorization state.
- A linkable `PathRef` or app deep link only after host authorization.

The bundled local text-semantic generation is required by the qualified default discovery profile but remains a rebuildable derivative. Alternate embeddings, CLIP, acoustic fingerprints, OCR, ASR, and external metadata are optional processors/artifacts. Deleting any of them affects discovery only; deleting the required local generation also makes the installation visibly degraded and non-conforming until it is rebuilt.

## 8. Capability grants and authorization

An app receives a short-lived, audience-bound `AppAccessGrant/v1` from the core:

~~~json
{
  "grant_ref": "grant:01K2...",
  "audience_app_id": "org.example.music-player",
  "principal_ref": "principal:local-user",
  "workspace_ref": "workspace:default",
  "scopes": ["catalog.read", "content.read", "annotation.write"],
  "subject_scope": {"subjects": ["subject:..."], "collections": ["collection:music"]},
  "representation_policy": "EXACT_OR_QUALIFIED_PLAYBACK",
  "max_bytes": "10737418240",
  "max_concurrent_handles": 4,
  "expires_at": "2026-08-12T04:00:00Z",
  "policy_revision": "policy:42",
  "nonce": "..."
}
~~~

The core signs or otherwise authenticates the grant. Handles issued under it bind the grant, subject/segment, representation, range, principal, expiry, and resource budget. Apps cannot widen scope, exchange a handle for another subject, infer credentials, or reuse a grant after policy or authorization revocation. Remote multi-user authentication (for example OIDC) is a later adapter profile; local deployments may use Unix identity and explicit workspace grants.

Derivative metadata and annotations inherit ACL/residency labels. Search counts, snippets, thumbnails, captions, embeddings, and progress records are filtered before disclosure.

## 9. External retrieval and game/application actions

“Find a game” and “download a game” are different product actions:

- `DISCOVER` returns source-attributed candidates and collection evidence.
- `PLAN_RETRIEVAL` creates a human-reviewable plan bound to immutable provider/version/edition/platform/architecture/locale and expected digests.
- `ACQUIRE` invokes a separately permissioned `RetrieverDriver` into quarantine, then independently hashes, validates, admits, and places bytes.
- `EXECUTE` or `INSTALL` is outside the generic retrieval contract and requires a separate platform profile, explicit user authority, and dynamic validation.

An app cannot fetch arbitrary URLs, magnets, stores, or launcher endpoints through `content.open`. P2P/magnet is merely one future `SourceBinding` variant and never proof of persistence, rights, or exact recovery.

## 10. Versioning, replacement, and failure behavior

- Domain schema revisions are immutable; incompatible revisions create new records or a migration generation.
- Changing a parser, model, transcode profile, time-base, field mapping, or segment algorithm creates new artifacts/index generations beside existing ones.
- Existing snapshots and annotations retain their original producer and schema references.
- Removing a domain pack removes its derived views and app capability, not exact bytes, paths, generic metadata, or recovery records.
- A stale or failed domain view is explicit. The core reports generic fallback rather than pretending a rich app view exists.
- A stream/transcode cache can be collected once no active session, retained collection, or recovery dependency references it.

## 11. Recommended implementation sequence

1. Finish `RW-CORE-1`: exact ingest, publication, `SnapshotTree`, `FileAccess`, verify, and restore.
2. Build `RW-CATALOG-1`: universal Inbox, generic item page, baseline lexical search, durable annotations, and virtual collections.
3. Qualify direct range streaming and ship a small `RW-AUDIO-1` client/domain pack.
4. Add `RW-BOOK-1` with chapters, progress, bookmarks, and notes.
5. Add video only after seek, transcode, subtitle, and resource-budget qualification.
6. Add image/document packs selectively or integrate existing applications through the same APIs.
7. Add read-only application/game collection resolution; defer retrieval and execution until source, rights, quarantine, and validation gates are proven.
8. Add alternate embeddings, CLIP, ASR, OCR, and external enrichment as optional rebuildable projections; the bundled local text embedding is already part of the core MVP gate.

Do not implement a universal player/reader/downloader binary. Implement the shared contracts and a compelling universal catalog first, then focused clients with narrow domain responsibility.

## 12. Acceptance tests

1. A mixed Inbox containing unknown binaries, music, books, video, archives, and game files remains exactly browsable and restorable when all domain packs are disabled.
2. A music client and book client use only `SubjectRef`, `SegmentRef`, `RepresentationRef`, collections, annotations, and scoped `AccessHandle` values; neither reads private paths or repository objects.
3. A direct exact read never returns an approximate or generated representation. A playback request reports any derived rendition and its contract.
4. A time/page segment can be opened, streamed, or read only within its authorized parent subject and snapshot.
5. Collection edits create successor revisions, preserve ordering, survive index rebuild, and do not duplicate or move physical content.
6. Playback progress, reading progress, bookmarks, and notes survive client replacement and export/import under the declared annotation schema.
7. A stale query result is reauthorized and labeled stale or suppressed before metadata/bytes are returned.
8. A domain-pack removal leaves generic namespace, exact access, verify, restore, and baseline search operational.
9. Retrieval requests produce plans and quarantine records; no app can turn a URL or magnet into an unreviewed placement or executable install.
10. App grants expire and cannot be replayed for another principal, subject, representation, range, or workspace.

## 13. Relationship to existing requirements

This contract specializes, but does not override:

- [Core Kernel and Interface Requirements](../requirements/core-kernel-and-interface.md) for authority and durable records.
- [Namespace and Content Access](namespace-and-content-access.md) for file-shaped browse and exact/range reads.
- [CLI and MCP Contract](../requirements/cli-and-mcp-contract.md) for transport-neutral commands and handles.
- [Extension System](../requirements/plugin-system.md) for signed capability packages and isolation.
- [Application and Game Collections](../requirements/application-and-game-collections.md) for later collection resolution and dependency closure.
- [External Source and Retrieval](../requirements/external-source-and-retrieval.md) for pinned acquisition and quarantine.

When this document conflicts with `RW-MVP-1`, the MVP remains authoritative; the interfaces here are activated only by a named catalog or domain profile.
