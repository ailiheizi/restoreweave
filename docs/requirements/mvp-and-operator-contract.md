# MVP and Operator Contract

## 1. Status and scope

This document freezes the first qualified product profile, `RW-MVP-1`. It specializes the [Product Requirements](product-requirements.md), follows the authority boundary in [Core Kernel and Interface Requirements](core-kernel-and-interface.md), uses the typed surfaces in the [CLI and MCP Contract](cli-and-mcp-contract.md), and fits within the [System Architecture](system-architecture.md).

`RW-MVP-1` is the first exact, self-hosted managed-archive and search profile for NAS and large heterogeneous filesystem collections. Its reference deployment is a Linux-based NAS or server. Platform-specific capture drivers qualify independently and do not redefine the product-wide release gate.

Where broader documents describe semantic, perceptual, neural, enterprise, or writable-NAS behavior, this document determines what is required for the first release.

## 2. One complete product job

RestoreWeave MVP performs one complete job:

> Connect a heterogeneous local or NAS-mounted tree, identify and process its contents through strong defaults, minimize exact storage, publish a searchable and recoverable filesystem view, and prove that view can be restored on a fresh installation.

The golden path is:

```text
preflight source, processors, working space, and repository
-> open a source capture with an explicit consistency claim
-> inventory namespace entries and exact content identity
-> keep mandatory exact placement independent from optional processors
-> collect suffix evidence, then magic-byte evidence
-> route every entry through a declared processing profile
-> build a progressive immutable storage and indexing plan
-> create a reviewed successor plan when weaker decisions are requested
-> apply exact placement through the reference repository
-> reconcile payload and namespace coverage
-> perform required repository placement and exact-lane readback verification
-> publish the prepared portable recovery closure
-> publish the signed portable commit marker
-> build the baseline metadata and content index
-> index durable tag and note records
-> perform deterministic sampled-content verification
-> browse, search, mount, and read the published namespace
-> restore selected paths or the full tree
-> compare exact bytes and declared filesystem metadata
```

This path must work without an AI model, embedding provider, CLIP provider, WebUI, REST service, agent runtime, or platform-specific snapshot API.

### 2.1 Managed-archive boundary

`RW-MVP-1` qualifies managed archive mode, not a writable primary NAS replacement. The source is read-only from RestoreWeave's perspective. The published original-path namespace is also read-only.

The MVP reports repository storage reduction and index overhead precisely. It MUST NOT describe logical duplicate bytes as released NAS capacity while the original source still exists. Releasing source capacity requires a separate migration and retirement profile with explicit human authority, verified exact recovery, placement sufficiency, rollback rules, and post-retirement recovery testing. Automatic source deletion is disabled in `RW-MVP-1`.

## 3. Reference profile

| Dimension | `RW-MVP-1` choice |
| --- | --- |
| Product shape | Self-hosted controller, CLI, and read-only Linux FUSE adapter, usable as a native process or container where qualified |
| Target operator | NAS, homelab, archive, and small technical-team operator |
| Source | One configured local or mounted filesystem root |
| Platform | NAS/server-oriented; no required vendor, Linux distribution, or filesystem |
| Capture | Generic read-only tree capture with explicit consistency evidence and mutation detection; native snapshot drivers are optional |
| Operational state | Local embedded catalog and journal; clean recovery cannot depend on them |
| Identification | Filename suffix evidence followed by magic-byte evidence |
| Processing | Qualified common metadata and text processors plus a generic exact route |
| Exact identity | SHA-256 and length for every published regular-file content identity |
| Repository | One qualified mature exact repository through `RepositoryDriver`; the release applicability matrix names the selected engine and version |
| Storage reduction | Exact duplicate grouping plus repository compression and deduplication |
| Namespace | Authenticated read-only `SnapshotTree` and bounded `FileAccess`, with a bundled single-principal, single-root, immutable-snapshot Linux FUSE projection |
| Search | Bundled SQLite FTS5 lexical projection over path, metadata, class, checksum, duplicate, tag, note, processing state, common media metadata, and extracted text; the SQLite schema is private and rebuildable |
| Annotations | Durable whole-subject tag and plain-text note CRUD with portable export |
| Interfaces | Human CLI, stable JSON/JSONL, and local read-only stdio MCP |
| AI | No embedded model, prompt loop, agent harness, or AI dependency |

An independently qualified snapshot-capable `CaptureDriver` may make a stronger point-in-time claim. The generic local or mounted-tree profile remains valid without any native snapshot driver and reports only the consistency it actually achieved.

## 4. Recovery outcomes and safety floor

Every selected namespace entry has one visible managed-ingest outcome:

| Outcome | Meaning |
| --- | --- |
| `EXACT` | A byte-exact recoverable representation was placed and admitted. |
| `EXACT_FALLBACK` | Readable bytes were preserved exactly because identification or processing was unknown, unsupported, conflicting, stale, unavailable, or failed. |
| `EXPLICITLY_UNPROTECTED` | A human or already-published policy deliberately accepted non-recoverability. |
| `BLOCKED` | The declared exact contract could not be satisfied, for example because bytes were unreadable or the source changed without a valid capture basis. |

`RW-MVP-1` publishes one source-recovery class: exact bytes. Repository compression and deduplication are physical implementation details and do not weaken that contract.

A filename suffix, magic result, learned classification, duplicate candidate, external URL, downloadable label, perceptual match, processor output, MCP request, or search result cannot authorize omission. Unknown readable content always follows the generic exact route.

## 5. Default identification and processing pipeline

The MVP must ship a useful processing pipeline. It cannot require operators to design a workflow graph before the first run.

The host-owned exact lane begins from inventory and continues independently through full-content hashing, duplicate accounting, exact placement, and readback. The identification and processing steps below run beside that lane. They may improve catalog coverage or add derived artifacts, but they cannot make readable bytes ineligible for exact fallback.

### 5.1 Evidence order

Each regular file's identification branch is evaluated in this order:

1. Record filename suffix and path-context evidence.
2. Inspect a bounded prefix or required ranges for magic bytes and container signatures.
3. Resolve a normalized content class while retaining all evidence and conflicts.
4. Invoke qualified structural processors for supported common formats.
5. Select the generic exact route when evidence is insufficient or processing fails.

Magic evidence may outweigh a suffix for routing, but it does not erase the suffix observation. Conflicts are visible in the plan, catalog, and processing provenance.

Learned or AI classification is not a default MVP dependency. A later learned detector uses the same evidence record and cannot override the safety floor.

### 5.2 Default routes

| Content class | Default processor result | Recoverability result |
| --- | --- | --- |
| Plain text and source | Charset and bounded text extraction, line and language hints where deterministic | Exact bytes |
| Common documents | Basic metadata and bounded extracted text when a qualified extractor supports the format | Exact bytes or exact fallback |
| Images | Dimensions, format, color, and embedded metadata where safely available | Exact bytes |
| Audio and video | Container, codec, duration, stream, and tag metadata where safely available | Exact bytes |
| Archives and packages | Container identity and bounded member inventory without uncontrolled recursive expansion | Exact bytes |
| Applications, games, models, databases, disk images, and opaque binaries | Type evidence and safe filesystem metadata only unless a qualified processor exists | Exact fallback |
| Unknown, encrypted, malformed, or processor-failed content | Typed warning and generic metadata | Exact fallback |

Processor outputs are derived artifacts. The source bytes remain the recovery authority in `RW-MVP-1`.

### 5.3 Processor behavior

The default processor host must provide:

- Capability discovery and exact implementation/configuration identity.
- Bounded immutable input handles.
- CPU, memory, temporary-disk, time, network, disclosure, recursion, and expansion limits.
- Cancellation and typed partial or failed results.
- Output-schema validation and provenance.
- No ambient filesystem, repository, credential, signing, or network access.

A processor crash, timeout, missing dependency, unsupported architecture, invalid schema, or low-confidence result on readable data produces exact fallback and a warning. It does not block the whole run.

## 6. Exact storage minimization

The MVP storage value comes from safe, measurable mechanisms:

- Completed exact content identity groups byte-identical files.
- Every original path remains a distinct namespace entry even when content is shared.
- The reference repository owns chunking, compression, encryption, pack layout, and physical deduplication.
- RestoreWeave records logical source bytes, exact duplicate bytes, estimated repository bytes, actual physical repository growth where observable, index overhead, and transfer bytes separately.
- Incremental runs reuse unchanged content and valid derived artifacts.

The plan must never combine these categories into one misleading savings number. Logical duplicate savings, repository compression, existing-backend reuse, explicit exclusion, and future alternate representations have different recovery consequences.

Class-specific lossless transforms may be exercised experimentally only outside the release claim unless their decoder dependency closure and host-controlled round-trip SHA-256 validation are qualified. Perceptual, neural, reacquirable, rebuildable, or generative representations cannot replace source bytes in `RW-MVP-1`.

## 7. Search and discovery baseline

Search is an MVP product surface, not a recovery dependency.

The baseline search index covers:

- Source, snapshot, directory, path, and filename.
- Entry type, content class, suffix evidence, magic evidence, and conflicts.
- Size, times, checksum, duplicate group, and processing state.
- Durable user-authored tags and note text.
- Qualified extracted text.
- Qualified image, audio, video, document, and container metadata.
- Warnings, processor provenance, and available exact representation state.

Every result returns a stable subject reference and resolves to the same snapshot namespace used by browse, read, and restore. Search must expose incomplete extraction coverage and stale index generations.

The bundled `RW-MVP-1` implementation uses SQLite FTS5 for its lexical `IndexProvider` and `QueryProvider`. Each immutable `IndexGenerationRef` owns one physically separate disposable database. The SQLite schema, row IDs, token tables, and query syntax are private implementation details, not portable records or public ABI. Deleting and rebuilding every index database must not change published content, namespace, tag or note revisions, plans, or verification evidence.

Embedding, CLIP, vector, hybrid, multimodal, and external-knowledge providers are a staged product expansion. They must attach to the same subjects and result model. Their absence from `RW-MVP-1` removes neither the semantic-search direction nor the processor and index boundaries required to add them later.

## 8. Recoverable namespace

Every published snapshot exposes one authenticated original-path view through `SnapshotTree` and `FileAccess`.

The baseline supports:

- Root lookup and component-by-component path resolution.
- Paginated directory listing.
- Metadata lookup and raw symbolic-link targets.
- Bounded and streaming reads for exact regular files.
- Original-path restore.
- Hard-link and symlink reconstruction where the source and destination profiles support them.
- Explicit metadata degradation for unsupported ownership, permissions, names, sparse regions, extended attributes, or other filesystem features.

Repository-private packs, chunks, object keys, and deduplication layout are never the user namespace. `RW-MVP-1` bundles a read-only Linux FUSE projection over the same `SnapshotTree` and `FileAccess` contracts. One mount binds one principal, one export root, and one immutable snapshot. It verifies `ro,nodev,nosuid,noexec`, refuses `allow_other` and arbitrary mount-option passthrough, supports lookup, scoped directory enumeration, attributes, raw representable names, symbolic-link reads, hard links, sparse-file semantics, bounded or streaming regular-file reads, collision-resolved mount-local stable inodes, and stable snapshot pinning, and returns `EROFS` for every write-capable open and mutation opcode. Cache, open-handle, page-cache, `mmap`, expiry, revocation, and unmount behavior are part of the qualified compatibility profile. If revocation cannot meet the declared bound, the mount is reported as a local-trust surface. Future SMB, NFS, WebDAV, S3, media-server, or alternate FUSE gateways must project the same namespace rather than inventing another mapping.

An explicitly unprotected entry may remain visible as a decision record, but it has no payload and is never presented as readable or recoverable.

## 9. Capture consistency and plan basis

A capture records one of the consistency claims supported by its driver. The core must preserve the distinction between an atomic snapshot and a validated live traversal.

For a retained immutable capture:

- Planning, processor reads, hashing, and apply bind the same capture digest.
- Plan and job holds prevent release while a consumer remains active.

For a live or mounted tree that cannot be retained:

- The plan records the weaker capture claim and exact per-entry revision evidence.
- Apply revalidates selected entries before and while streaming them.
- A changed entry is retried only within a bounded rule; unresolved drift receives a new plan requirement or `BLOCKED` outcome.
- The system must not claim a collection-wide point-in-time state it did not capture.

The published namespace binds the bytes actually admitted during apply and the exact capture-consistency statement. A weaker source capture does not weaken byte equality for successfully admitted files, but it may prevent a stronger application-consistency or global point-in-time claim.

## 10. Plan, apply, and incremental lifecycle

Ingest planning performs no repository write. Restore planning performs no destination write.

An ingest plan records:

- Source and capture basis.
- Complete namespace accounting.
- Identification evidence and conflicts.
- Processor routes, coverage, provenance, and failures.
- Exact duplicate groups and storage estimates.
- Selected exact representations and repository target.
- Indexing scope and expected overhead.
- Verification work and recovery claim.
- Explicit exclusions, blocked entries, and required decisions.
- Canonical plan digest and freshness boundary.

Review never mutates a plan. It creates a successor that binds the base reference and digest, candidate decisions, decision authority, and a new digest.

Apply accepts the immutable plan and exact digest. It rejects a missing or mismatched digest, changed capture or source basis, changed processor contract where results are plan-relevant, changed repository identity, expired plan, or changed restore destination.

The automatic recurring path may apply only monotonic exact changes with no new exclusion, lossiness, deletion, weaker placement, or unresolved risk. Otherwise it stores a reviewable plan and reports `BLOCKED` for unattended application.

Incremental behavior must preserve historical meaning:

- Unchanged exact content may reuse placement and valid processor artifacts.
- Changed bytes create a new content identity and namespace generation.
- Source deletion affects only the new generation and does not erase prior published snapshots.
- A processor upgrade creates a new artifact or index generation and does not reinterpret old output in place.
- A representation migration verifies the new placement before the old placement may become a retention candidate.
- Source deletion and destructive repository pruning remain disabled by default.

## 11. Repository and publication contract

`RW-MVP-1` requires one qualified exact repository, not two. This requirements profile does not preselect the engine. Each release MUST name and qualify the exact engine, version, reader dependencies, and backend profile it ships, and the private format never becomes RestoreWeave namespace or content identity.

One repository may support the full baseline flow and may be reported as:

- `PROTECTED_SINGLE_REPOSITORY`
- `REPOSITORY_VERIFIED`
- `RESTORE_VERIFIED`

It must not be reported as redundant, provider-independent, or resilient to repository loss. A target in the same failure domain as the source is explicitly at risk. A separate machine or remote target is the onboarding recommendation, while multiple failure-independent placements belong to a later profile.

The repository driver provides storage, lookup, readback, verification, restore access, durable receipts, and reconciliation. RestoreWeave owns logical snapshot publication and recovery meaning.

The portable publication sequence is:

```text
place and reconcile PAYLOAD
-> compare admitted namespace and selection
-> complete authenticated-metadata verification
-> prepare and sign the Recovery Record Format root
-> place and reconcile PREPARED_CLOSURE
-> create and sign PublicationCommitRecord
-> place and reconcile PUBLICATION_COMMIT
-> expose the logical snapshot
```

The `PublicationCommitRecord` binds the RRF root, payload receipt, prepared-closure receipt, plan digest, capture or applied-inventory digest, authenticated-metadata evidence, publication generation, and fence. It does not need to contain its own placement receipt.

`PUBLICATION_COMMIT` is the portable logical commit point. Orphan payloads and prepared closures are not published snapshots. The operational catalog is a rebuildable projection of valid signed publication records.

## 12. Verification and clean-install recovery

The verification levels are:

1. Authenticated metadata and publication closure.
2. Deterministic sampled-content readback.
3. Full-byte readback.
4. Exact test restore.
5. Clean-install recovery drill.

The default apply completes the authenticated-metadata gate and deterministic sampled-content verification before returning a healthy content-verification result. Full-byte verification and recovery drills are explicit or externally scheduled.

Every successful publication must make an authenticated Recovery Record Format closure retrievable without the operational catalog. It binds:

- Repository and snapshot identity.
- Source-root-to-restored-path mapping.
- Accepted plan digest and recovery contract.
- Protected, explicitly unprotected, and blocked entries.
- Content digests, representation records, dependencies, and repository receipts.
- Filesystem fidelity and capture-consistency claims.
- RestoreWeave, driver, repository, processor-schema, and reader compatibility information.

A clean-install restore may require only a compatible RestoreWeave reader, repository target, scoped credential source, independent trust anchor, signed publication record, and supported destination. It must not require the original source, catalog, search index, processor binaries, plugin registry, AI harness, model registry, MCP client, REST service, or WebUI.

Only a completed restore with exact post-restore digest comparison may report `RESTORE_VERIFIED`. Upload completion alone cannot report verified recovery.

## 13. Operator surface

The first command family is:

```text
restoreweave doctor [<source>] [--to <target>] [--credential <credential-ref>]
restoreweave plan <source> --to <target> [--credential <credential-ref>] [--save-profile <name>]
restoreweave plan revise <base-plan-ref> --digest <base-plan-digest> [--decisions <json-file>]
restoreweave apply <plan-ref> --digest <plan-digest>
restoreweave profile run <profile-name>
restoreweave status [<job-or-snapshot-ref>] [--events] [--after <sequence>] [--limit <count>]
restoreweave search <query> [--snapshot <snapshot-ref>]
restoreweave tag add <subject-ref> <tag> [--expected-revision <revision>]
restoreweave tag remove <subject-ref> <tag> [--expected-revision <revision>]
restoreweave note set <subject-ref> --from-file <file> [--expected-revision <revision>]
restoreweave note remove <subject-ref> [--expected-revision <revision>]
restoreweave browse [<snapshot-ref>[:<path>]]
restoreweave cat <snapshot-ref>:<path> [--to-file <new-file>]
restoreweave mount <snapshot-ref> <mountpoint> [--foreground]
restoreweave verify <snapshot-ref> [--mode authenticated-metadata|sampled-content|full-bytes]
restoreweave recovery export <snapshot-ref> --to-file <new-file>
restoreweave restore <snapshot-ref>:<path> <destination>
restoreweave restore --from <target> --recovery-reference <file> --to <destination> --credential <credential-ref>
restoreweave mcp serve --stdio
```

Every non-raw-content command supports `--format human|json|jsonl`. Human output is rendered from the same typed result as machine output.

- `doctor` checks source accessibility, capture consistency, processor health and sandbox requirements, working space, repository identity, credential readiness, and clean-recovery prerequisites. Optional platform-driver failures are scoped to that driver.
- `plan` creates an immutable ingest plan and reports storage savings, processing coverage, search coverage, recovery consequences, and blocked decisions.
- `plan revise` creates an immutable successor after explicit decisions.
- `apply` performs the declared repository or restore mutation after revalidation.
- `profile run` creates a fresh plan and auto-applies only monotonic exact changes.
- `status` separates processing coverage, storage savings, index freshness, placement, verification, and required action.
- `search` queries the baseline metadata and extracted-content index and returns subject and namespace references.
- `tag` and `note` mutate durable subject-bound annotation records through optimistic revision checks; they do not write disposable index rows directly.
- `browse` and `cat` use the authenticated snapshot namespace rather than repository-private paths.
- `mount` starts the bundled read-only Linux FUSE projection pinned to one immutable snapshot.
- `verify` records the selected evidence level without relabeling sampled work as full verification.
- `recovery export` produces an independently retainable recovery reference without plaintext credentials or private signing keys.
- `restore` creates and applies an exact restore plan; non-interactive and machine clients retain explicit plan/apply separation.
- `mcp serve` opens only local stdio and no network listener.

The closed core result statuses remain `ACCEPTED`, `SUCCEEDED`, `DEGRADED`, `BLOCKED`, `FAILED`, `CANCELLED`, and `UNKNOWN_EXTERNAL_OUTCOME`. Durable job events are ordered and resumable through `job.events(job_ref, after_sequence, limit)`.

### 13.1 Read-only MCP profile

The qualified MCP profile exposes bounded read operations for health, status, existing plans, search, namespace metadata, processing provenance, verification evidence, and other explicitly safe inspection surfaces.

It does not expose repository mutation, restore-destination mutation, source deletion, policy mutation, processor installation, arbitrary job control, ambient credentials, arbitrary shell text, or unrestricted filesystem reads.

External automation or AI harnesses can inspect existing subjects and plans and may construct proposals outside RestoreWeave. The initial MCP profile cannot create, revise, approve, abandon, or apply a plan, mutate an annotation, cancel a job, initialize or prune a repository, restore a destination, or invoke an arbitrary processor. Mutation remains a CLI or future separately qualified and explicitly granted operation. RestoreWeave contains no embedded general AI harness.

## 14. Explicitly staged capabilities

The following are important product directions but not dependencies of the first exact release:

- Learned or AI file identification.
- OCR and ASR beyond the qualified default extraction set.
- Embeddings, CLIP, vector search, hybrid ranking, multimodal retrieval, and external knowledge enrichment.
- Collections, ratings, relationship graphs, recovery-intent services, and machine-suggested annotation workflows.
- Alternate repository engines and multiple failure-independent placements.
- Lossless class-specific codecs.
- Perceptual image, audio, or video representations.
- Neural, VAE, RWKV-style, or foundation-codec representations.
- Authoritative reacquisition, rebuild, P2P, magnet, or swarm profiles.
- SMB, NFS, WebDAV, S3, media-server, alternate FUSE, and writable gateways.
- A WebUI, remote REST adapter, mobile client, or enterprise control service.
- Application-consistent databases, virtual machines, containers, applications, games, or bare-metal images.
- Writable NAS behavior, synchronization, source deletion, automatic pruning, and destructive garbage collection.
- High availability, multitenancy, enterprise RBAC, legal hold, or compliance operation.

Staged semantic capabilities must use the same subjects, processor provenance, index generations, and namespace resolution defined by the MVP. They do not create a separate AI product inside RestoreWeave.

## 15. Acceptance tests

`RW-MVP-1` must pass all of the following:

1. A fresh self-hosted installation completes doctor, plan, apply, sampled verification, baseline search, browse, recovery export, and exact restore through the documented CLI against one qualified repository.
2. The qualification corpus includes both a local filesystem root and a mounted NAS-like root. Each receives an honest capture-consistency result.
3. The product completes the reference flow on the qualified Linux/NAS environment without any optional platform-specific capture helper.
4. Every selected namespace entry is accounted for as `EXACT`, `EXACT_FALLBACK`, `EXPLICITLY_UNPROTECTED`, or `BLOCKED`.
5. Suffix evidence is recorded before magic-byte evidence, both remain inspectable, and a conflict produces a visible route decision rather than silent evidence loss.
6. Each supported content class enters its declared default route; unknown content enters the generic exact route.
7. A missing, crashed, timed-out, incompatible, or malformed optional processor on readable data produces exact fallback and does not prevent exact ingest and publication.
8. Disabling every optional AI, semantic, or learned component leaves inventory, exact storage, search over baseline fields, browse, verification, and restore functional.
9. Exact duplicate files preserve every original path while sharing one content identity and benefiting from repository deduplication where supported.
10. Reports separate logical duplicate bytes, compression or repository reuse, actual physical growth, index overhead, transfer, explicit exclusion, and future-representation estimates.
11. Unknown, encrypted, malformed, and opaque test files restore byte-for-byte when readable.
12. Planning writes no repository data and restore planning writes no destination data.
13. Apply rejects a missing or mismatched digest, stale capture or source basis, changed plan-relevant processor contract, changed repository identity, changed destination, or expired plan.
14. A processor result, search result, MCP request, or conversational response alone cannot exclude an entry, select a weaker representation, delete source data, or accept verification.
15. Interruption at every publication boundary leaves no false published state. Reconciliation creates at most one logical publication; equivalent physical duplicates are recorded and conflicting duplicates block.
16. Projection rebuild exposes only snapshots with a valid signed `PUBLICATION_COMMIT`; orphan payload and `PREPARED_CLOSURE` placements remain unpublished.
17. Injected repository corruption causes the applicable verification to fail and prevents a verified health claim.
18. A clean installation with no original catalog, index, processor binaries, plugin registry, AI service, MCP client, REST service, or WebUI authenticates the portable publication and restores exact bytes.
19. Every restored regular file matches its recorded SHA-256 and length. Supported paths, links, permissions, and other declared metadata match; degradation is explicit.
20. Baseline search finds qualified subjects by path, type, checksum, duplicate group, declared metadata, and extracted text, then resolves them to the same namespace used by browse and restore.
21. Search reports stale or incomplete generations. Deleting and rebuilding the index does not change snapshot, namespace, representation, or verification records.
22. An incremental run reuses unchanged valid content and artifacts, creates new versions for changed bytes, and preserves earlier snapshot meaning.
23. A source-path deletion appears only in the later namespace generation and does not erase the path from an earlier published snapshot.
24. Upgrading a processor creates new provenance and rebuilds only affected derived artifacts; it does not reinterpret old outputs in place.
25. A live-source mutation during scan or apply is detected where the declared driver contract says it is detectable, and unresolved drift cannot receive an atomic or complete claim.
26. A snapshot-capable optional driver can make a stronger consistency claim without changing the generic namespace and recovery semantics.
27. Failure of any optional platform driver blocks only that profile and never the general NAS-oriented release.
28. One repository is reported as one placement. Loss of that repository removes recoverability and never produces a redundancy claim.
29. The default MCP opens no listener and exposes no repository, restore, deletion, policy, installation, or arbitrary job-control mutation.
30. Equivalent CLI JSON and MCP read requests produce equivalent subjects, reasons, processing states, index-generation references, and verification facts.
31. Source deletion and destructive repository pruning are disabled in the reference profile.
32. Ordered durable job events resume after client disconnect through both CLI JSON and the applicable read-only MCP event surface.
33. Whole-subject tags and notes support create, read/list, update, tombstoned delete, portable export, and re-import with exact revision and SubjectRef preservation. Deleting every index loses none of them.
34. The bundled Linux FUSE view binds one principal, one export root, and one immutable snapshot; verifies `ro,nodev,nosuid,noexec` with no `allow_other`; and lists and reads the same raw names, paths, metadata, hard links, sparse files, symbolic links, and exact bytes as `SnapshotTree`, CLI browse, and content reads. Collision-resolved inodes remain stable for the mount, directory cookies cannot be replayed across handles or scopes, and every write-capable open and mutation opcode returns `EROFS` without side effects. Qualification exercises cache reuse, concurrent handles, authorization expiry and revocation through open handles, page cache, and `mmap`, plus clean and crash-driven unmount.
35. Capture qualification replaces each ancestor in turn with a symlink, renamed directory, bind mount, remounted share, and different filesystem object; removes or substitutes snapshot lifecycle protection before consumer start; and races regular files with FIFO or device-like objects under a deadline. The capture either remains bound to the originally validated object or blocks explicitly, never reads the replacement, never escapes the configured scope, and never publishes an unsafe observation. A final-component-only no-follow implementation fails qualification.
36. A filesystem watcher or journal is only a change hint. Non-recursive coverage, overflow, reset, rollback, truncation, loss, uncertain continuity, or use on an unqualified NFS, SMB, or FUSE source invalidates the incremental checkpoint and requires a complete baseline before absence can become deletion evidence.
37. A snapshot-backed capture remains publishable only while its recorded root, mount, filesystem or volume identity, snapshot identity, read-only state, retention hold or deletion protection, and traversal properties revalidate for every required consumer.

## 16. Success metrics and release gate

### 16.1 Release-quality targets

- 100 percent selected-entry accounting.
- Zero silent omissions.
- 100 percent exact fallback under processor failure or ambiguous classification when bytes remain readable.
- 100 percent exact byte matches across the supported restore corpus.
- 100 percent detection of injected corruption within the declared verification coverage.
- No published or verified state without the required placement, closure, commit, and verification evidence.
- No platform-specific optional driver is a global release dependency.

### 16.2 Product-value targets

- At least 80 percent of target NAS operators understand what is stored, deduplicated, unsupported, blocked, and recoverable from the primary report.
- At least 70 percent find a known item by metadata or content terms without knowing its path.
- At least 60 percent discover a duplicate, stale copy, missing exact placement or recoverability gap, or useful content relationship they did not already know.
- Exact physical storage reduction is measured across representative heterogeneous datasets and attributed separately to duplicate grouping, compression, and backend reuse.
- At least 80 percent complete a managed ingest and exact restore without project-team assistance.
- At least 80 percent can create, export, re-import, and find a tag or note without a semantic provider.
- At least 80 percent successfully rebuild the baseline index or recover after loss of the operational catalog using documented procedures.

### 16.3 Operational targets

- Steady-state exact apply throughput remains within 20 percent of the direct reference repository after reusable inventory and digest state are warm.
- Incremental processing avoids re-reading and reprocessing unchanged subjects when provenance remains valid.
- Baseline search makes freshness and coverage visible and returns useful results within the declared qualification envelope.
- Interrupted apply, verify, indexing, and restore jobs resume or reconcile without contradictory publications.
- A recurring exact profile can be invoked by an external scheduler and blocks for review whenever a weaker or unresolved decision appears.
- FUSE qualification publishes cold and warm first-byte latency, large-directory `readdir` and qualified `READDIRPLUS` scaling, sequential and random-read throughput, repository request and byte amplification, process and kernel-cache memory, concurrent file and directory handle limits, cache behavior, revocation residuals, and clean and crash-driven unmount results for the declared tuple.

The release is not successful if it proves only that files can be sent to a repository. It must demonstrate the combined product: measurable exact storage savings, useful heterogeneous discovery, a recoverable original namespace, replaceable processing, and verified restore.
