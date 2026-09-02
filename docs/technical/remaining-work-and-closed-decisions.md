# Remaining Work and Closed Decisions

> **Status:** Informative checkout record, updated 2026-08-25. The normative
> requirements and [Core MVP Execution Plan](core-mvp-execution-plan.md) remain
> authoritative. This file summarizes the current frontier; it cannot add
> scope, qualify a release, or select a repository engine.

## 1. Current product boundary

RestoreWeave is a content protection, description, discovery, export, and
recovery plane. It is not a writable NAS, mount service, media server, OpenList
fork, or client suite.

The ordinary loop is:

```text
configure
-> inspect and protect content
-> retain names, facts, one Notes surface, and recovery references
-> search lexical + structured + semantic information
-> save a view and freeze an ExportManifest
-> materialize and verify output
-> restore exact bytes
```

The core owns content identity, protection outcomes, plans, publications,
durable facts and descriptions, search-generation truth, views, exports,
verification, and recovery meaning. Replaceable repositories, processors, and
indexes may implement work behind those contracts; they do not redefine it.

## 2. Closed decisions

| Decision | Current rule |
| --- | --- |
| Exact identity | Full logical-stream SHA-256 plus logical length |
| Default deduplication | Exact whole-content deduplication |
| Default protection | `STORE_EXACT` |
| Search authority | Embeddings and indexes are rebuildable and never authorize deletion |
| Operational metadata | Keep one configured catalog; do not add a second live metadata database without a demonstrated need |
| Recovery authority | Repository objects plus authenticated portable recovery records and an independently retained trust anchor |
| Personal semantic profile | Pinned local BGE/ONNX + in-process zvec; no Qdrant, Milvus, or Compose dependency |
| User-facing free-form text | One `Notes` surface in the browser; durable `Annotation.NOTE` and `DescriptionDocument` records remain separate for provenance, revision, and recovery |
| Garbage collection | Non-destructive reachability planning only; no deletion executor |
| Source deletion | Disabled; any future capacity release requires a reviewed migration profile and human approval |
| Filesystem gateways | No built-in FUSE, SMB, NFS, WebDAV, S3, or mount product |
| OpenList | Research or external interoperation only; never the core, storage engine, or fork base |
| Neural/RWKV codecs | Deferred research only after simple lossless storage and recovery qualification |

The old Inbox, OpenSubsonic, and OPDS compatibility experiments have been
removed. The bounded loopback `/api/v1` adapter and React client call the same
typed dispatcher and do not create another catalog, job system, or recovery
model.

## 3. Implemented and tested development scope

- Strict persisted config and explicit catalog/repository/vector/model/recovery
  paths.
- Rooted directory inspection, immutable plans, digest-bound apply, source
  revalidation, idempotent replay, and visible blocked/degraded outcomes.
- Exact SHA-256 identity, whole-file duplicate placement, exact readback, and
  original-path recovery projection.
- Durable revisioned Notes and tags, provenance-preserving Descriptions and
  semantic segments, and basic text/audio-tag/EPUB extraction. The browser
  presents the free-form text through one Notes surface.
- Complete stated lexical/structured search fields and filters, source-bearing
  segment hits, and real provisioned BGE/ONNX + zvec fusion with honest
  degradation.
- Snapshot list/diff/verify, bounded content reads, dynamic SavedViews, frozen
  ExportManifests, materialization, verification, and exact restore.
- Signed portable recovery facts and subject mappings, independent trust
  anchors, clean-install readers, tamper rejection, no-follow reads,
  cross-process fencing, unknown-outcome reconciliation, and bounded retry of
  the same signed processor plan.
- Optional loopback API, focused browser client, maintenance/recovery CLI, and
  read-only local MCP.

This is a substantial core preview. It is not `RW-MVP-1` completion or release
qualification.

## 4. Runnable candidates

`directory-cas-dev-v1` remains the generated development repository.

`local-zstd-v1` is an opt-in candidate with whole-file compression and dedup,
decode-and-hash verification, corruption and relocation handling, repair,
mechanism-separated savings reports, and copy-forward migration evidence. It
is not a selected production engine.

`local-zstd-encrypted-v1` is an experimental candidate with AES-256-GCM,
host-provided key references, wrong/missing-key rejection, encrypted readback,
relocation, and key-rotation copy-forward evidence. It is not admitted by the
generated default and does not close encryption release qualification.

## 5. Near-term release work

1. **Formal asynchronous processing:** release-qualify the existing bounded
   same-plan worker, and define a separate signed successor contract for
   user-triggered, rerouted, or general reprocessing.
2. **Supported offline semantic packaging:** distribute the daemon, ONNX
   Runtime, pinned BGE model/tokenizer, and zvec library without a first-query
   download.
3. **Production repository qualification:** measure representative corpora and
   select one lossless profile only after encryption, crash, corruption,
   repair, relocation, migration, rollback, clean-reader, capacity, and net
   savings gates pass.
4. **Complete operator experience:** expose configuration, diagnostics,
   SavedViews, ExportManifests, backup, upgrade, and recovery guidance without
   requiring internal IDs in the ordinary browser flow.
5. **Release qualification:** run Linux and NAS-like acceptance for search
   coverage, latency, storage growth, recovery time, upgrade/rollback,
   packaging, and clean installation.

## 6. Later optional directions

These directions require concrete user demand and a separately admitted
profile. They are not current release promises:

- reviewed source migration and human-approved capacity release;
- richer extractors, OCR, ASR, CLIP, and alternate embedding profiles;
- additional qualified repositories, independent copies, and tiering;
- remote enterprise administration, audit, RBAC, HA, and multitenancy;
- advanced exact-reversible representations after the simple storage and
  recovery gates close.

## 7. Not in the core queue

- Built-in filesystem or network-storage gateways.
- An OpenList fork or alternate path-first core.
- Embedded players, readers, or media applications.
- Automatic external reacquisition or autonomous source deletion.
- Destructive GC before a separately authorized and qualified lifecycle
  profile; the current implementation remains `NON_DESTRUCTIVE_ONLY`.
- New acoustic, graph, or multimodal default dimensions during the current
  implementation lock.
- RWKV, Transformer, arithmetic, or other neural codec implementation before
  lossless repository and recovery qualification.

## 8. Status vocabulary

- **implemented and tested:** executable evidence exists for the stated scope;
- **candidate:** runnable and measurable, but not release-qualified;
- **release default:** mandatory behavior of a qualified distribution;
- **planned:** no shipped capability claim;
- **closed non-goal:** intentionally outside the current product.

A fixture, interface, test handshake, or provisioned local component is not a
supported installer or a production-qualified release by itself.
