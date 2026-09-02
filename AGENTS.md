# RestoreWeave Agent Guardrails

This file applies to the entire repository. Read it before planning, editing, or
delegating work. Preserve unrelated worktree changes.

## Product North Star

RestoreWeave is a content, description, discovery, export, and recovery plane.
It is not a normal filesystem, mount service, media application, or OpenList
fork.

The ordinary user loop is:

```text
configure
-> inspect and protect content
-> retain names, facts, descriptions, and recovery references
-> search lexical + structured + semantic information
-> select a LinkGroup or SavedView
-> freeze an ExportManifest
-> materialize and verify output
-> restore exact bytes when needed
```

Original paths are provenance and recovery projections. Organization belongs
to stable subjects, tags, descriptions, minimal link groups, relations, and
saved views. Normal workflows must not require users to know workspace, root,
entry, artifact, or other internal IDs.

If a proposed change neither improves this loop nor closes one of its recovery
or safety gates, keep it outside the core queue.

## Frozen Decisions

- Exact identity is the SHA-256 digest of the complete logical byte stream plus
  its logical length.
- Default deduplication is exact whole-content deduplication. Embeddings,
  similarity, perceptual hashes, filenames, metadata, and RWKV/Transformer
  output never establish exact identity or authorize deletion.
- `STORE_EXACT` is the default protection mode.
- `LINK_ONLY` requires an explicit decision and remains visibly
  `LINK_ONLY_UNPROTECTED` until independently reacquired and verified.
- Unknown or failed derived processing does not block readable exact
  preservation. It produces an explicit fallback or unavailable outcome.
- The personal default discovery profile includes real local semantic search:
  pinned ONNX Runtime, `BAAI/bge-small-zh-v1.5`, and in-process zvec. Fixture
  vectors do not satisfy this requirement. Qdrant, Milvus, and Docker Compose
  are not personal-profile dependencies.
- User, imported, extracted, and model-produced descriptions are durable,
  versioned content. AI description generation is an explicit, on-demand
  `DESCRIBE_SUBJECT` operation and is not an implicit ingest dependency.
- The user-facing free-form text concept is one `Notes` surface. User-authored,
  imported, extracted, and model-produced text is shown there with an optional
  source label; do not add a separate Description screen, setting, or search
  concept. Keep the durable source records distinct when provenance, acceptance,
  or recovery evidence needs them, but do not copy AI text into a second
  `Annotation.NOTE` merely for presentation.
- Daily organization is content-first and multi-tag-first. Source directories
  remain provenance and recovery projections. Reuse durable `Annotation.TAG`
  facts for user tags; deterministic type and format facets remain system
  fields. Machine classification must be explicit, attributable, previewed,
  and confirmed before it changes user-visible tags.
- The only planned MVP grouping primitive is a minimal `LinkGroup`: one stable
  group subject plus a current map from safe group-relative paths to stable
  file links. A file may appear in multiple groups without copying bytes.
  Changes update the current map; the group has no user-visible versions or
  revision chain. When a fixed point in time is needed, `ExportManifest`
  freezes the selected file versions separately. Group descriptions, tags,
  notes, search, and export reuse the existing subject mechanisms. Membership
  is not ownership and never authorizes byte deletion. A missing member stays
  visible as missing/unavailable, and an empty group remains until explicitly
  deleted. The initial profile has file members only: no nested groups, roles,
  dependency graph, or automatic AI grouping. Richer Collections remain
  deferred.
- The operational catalog and every search index are rebuildable projections.
  Repository objects plus authenticated portable recovery records are the
  recovery authority.
- Keep the physical core small: one configured SQLite catalog may hold the
  durable metadata and rebuildable search tables; the content repository holds
  file bytes and admitted representations. Do not add a second live metadata
  or recovery database merely to mirror the logical layers. Signed portable
  recovery records are backup artifacts emitted from the catalog/publication,
  not another operational service.
- Do not add complexity for hypothetical failures. Add a new durable store,
  service, or recovery mechanism only when a concrete user workflow requires
  it and an executable test demonstrates the need.
- Experimental RWKV/Transformer compression is never enabled silently. Until
  its exact-reversible profile is qualified, it remains opt-in and must show a
  clear warning that missing database/recovery backups or decoder dependencies
  can make compressed data unrecoverable.
- Storage, catalog, vector, and recovery locations come from persisted config.
  They never implicitly use a client process's current directory. `.` is an
  input source only when the user explicitly supplies it.
- Repository, embedding, processor, and index implementations are replaceable
  behind the narrow contracts in
  `docs/requirements/driver-and-processor-interfaces.md`. Their profile and
  version digests must be recorded; replacement cannot reinterpret old facts.
- FUSE/mount services, network filesystems, neural codecs, and external
  reacquisition are separate future or foreign-tool concerns, not hidden core
  dependencies.

## Documentation Authority

Resolve conflicts in this order:

1. `docs/requirements/mvp-and-operator-contract.md`
2. `docs/requirements/product-requirements.md`
3. `docs/requirements/content-store-views-and-exports.md`
4. `docs/requirements/core-kernel-and-interface.md`
5. Topic-specific normative requirements, including recovery, security, CLI,
   and release qualification contracts
6. `docs/technical/core-mvp-execution-plan.md`, the only implementation order
7. Other technical plans, completion plans, fixture documents, and research

`docs/README.md` is the reading map. Informative documents can explain a
decision but cannot add MVP scope, reorder phases, weaken a normative contract,
or award completion credit. Recovery evidence details belong to
`restore-manifest.md` and `recovery-fidelity.md`; content, deduplication,
discovery, view/export, and GC semantics belong to the content-store contract.

## Current Implementation Lock

The admitted Phase 3 Recovery Closure profile is implemented and tested for
the stated development scope. It is not release-qualified. Work in this order:

1. Complete the admitted xattr/ACL/sparse/detection fact profile and bind
   description, annotation, processor-artifact, and portable subject-mapping
   records into the authenticated closure. Terminal processor attempts use a
   signed complete-state successor chain. The admitted worker may perform only
   bounded automatic retry of the same signed plan with retry intent,
   idempotency, reconciliation, fencing, and retry ceilings bound to that
   tested lineage. Do not enable arbitrary manual, rerouted, or general
   reprocessing without a separately reviewed successor contract.
2. Complete independently retained trust-anchor handling and the clean-install
   import/reader workflow.
3. Add cross-process publication fencing or leases and unknown-outcome
   reconciliation.
4. Prove corruption rejection, relocation, and reader-dependency behavior for
   every admitted portable record.

Phase 4 and Phase 5 may start in parallel only after the Phase 3 portable
record shapes are frozen. Phase 4 supplies real ONNX/BGE + zvec, complete
search fields, segment provenance, and fused query. Phase 5 measures and
qualifies simple recoverable storage savings. Phase 6 supplies minimal
LinkGroup, SavedView, and ExportManifest ergonomics. This documentation
decision does not authorize LinkGroup schema or API implementation before the
Phase 4 and Phase 5 gates close. Phase 7 supplies packaging, migration,
backup, upgrade, performance, and full release qualification.

The following remain outside the core queue:

- FUSE, mount, SMB, NFS, WebDAV, or S3 gateway behavior;
- an OpenList fork, dependency, or alternate core;
- new Inbox, OpenSubsonic, OPDS, player, or domain-reader surfaces; the
  portable Phase 3 recovery reader is required core work. A bounded local
  `/api/v1` adapter and browser client may call the typed dispatcher, but they
  must not add a second policy, job, catalog, or recovery state machine;
- new acoustic, graph, multimodal, or other index dimensions;
- richer Collections, nested groups, dependency graphs, roles, ratings, or
  automatic grouping beyond the minimal planned LinkGroup;
- RWKV, Transformer, arithmetic, or other neural codec implementations;
- automatic external download/reacquisition, source deletion, or destructive
  garbage collection.

Existing adapters are maintenance-only. A fixture, mock, protocol handshake,
interface, or signed foundation is not a shipped capability or a closed phase.

## Mandatory Work Checkpoints

Before every implementation slice:

1. Read `docs/README.md`,
   `docs/requirements/content-store-views-and-exports.md`, and
   `docs/technical/core-mvp-execution-plan.md`.
2. Inspect the worktree and preserve changes not owned by the slice.
3. State one active phase, one normative requirement, and one executable exit
   test that the slice advances.
4. Confirm that the slice is inside the current lock and does not create a new
   product surface.

Re-read the core execution plan at these boundaries:

- before changing a durable schema, public command, provider interface, or
  signed format;
- before starting the next implementation slice;
- after a test reveals that the planned contract is incomplete;
- before changing any status matrix or calling work complete.

Before finishing a slice:

1. Run focused tests proportional to the change, then the broadest practical
   Go test set.
2. Run `git diff --check`.
3. Verify config, paths, enums, signed fields, and profile digests against the
   normative documents.
4. Check that failures and unavailable providers remain visible and fail
   closed where recovery meaning is affected.
5. Check that no fixture or harness is reported as a real default capability.
6. Update status prose only when executable evidence changed. Use
   `implemented` for tested implementation and `qualified` only after the
   stated release gate passes.

Every delegated task must include these guardrails, its allowed file/scope
boundary, and its phase/requirement/exit-test triple. Agents must report
conflicts instead of silently broadening scope.
