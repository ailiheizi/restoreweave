# Operations and Lifecycle Requirements

> **Profile status:** This document is an extended operations and enterprise-hardening design. `RW-MVP-1` is the single-node Linux/NAS managed-archive profile defined by the active core requirements. It owns generic honest capture, deterministic identification, bounded default metadata/text processing, exact fallback for readable unknown or failed content, exact hashing and duplicate accounting, one mature exact deduplicating and compressing repository engine, hybrid lexical/structured/local-semantic search, saved views and export manifests, a portable namespace, the finite operation journal, signed RRF publication, and clean-install restore. Platform- and engine-specific implementations qualify independently and are never global gates. Scheduling, resident daemons, fleet management, REST, WebUI, legal holds, advanced custody, multiple placements, independent monitoring, remote semantic services, and destructive lifecycle automation remain later phases. The Capsule Core, ControlPlaneRecoverySet, Recovery Bootstrap Envelope, RecoveryHeadWitness, Recovery Bootstrap Seed, and `COLD_VERIFIED` material below is retained only for separately named extended profiles.

## 1. Purpose

RestoreWeave must remain correct after years of upgrades, migrations, partial failures, expired credentials, offline sources, codec retirement, database loss, and human policy changes.

This document defines the operational state machines and transaction boundaries that are not visible in a static recovery contract.

Protection-objective compilation, policy precedence, retention rules, identity resolution, cost accounting, health derivation, and drill coverage are normative in [Protection Policy and Planning Requirements](protection-policy-and-planning.md). The frozen first implementation is defined in [MVP and Operator Contract](mvp-and-operator-contract.md).

## 2. Repository ownership boundary

The long-term architecture distinguishes two execution models. `RW-MVP-1` qualifies only engine-managed exact repository objects through one mature deduplicating and compressing engine.

### 2.1 Engine-managed objects

An external backup engine owns:

- Content-defined chunking.
- Deduplication.
- Compression.
- Encryption.
- Repository pack layout.
- Internal object identifiers.

RestoreWeave owns:

- Logical observations and recovery contracts.
- Stable representation identities.
- `RepositoryDriver` profile, configuration, and receipts.
- Required reader and repository dependencies.
- Independent restore verification.
- Export and migration plans.

`RW-MVP-1` uses this model through one qualified `RepositoryDriver` controlling one repository, always reported as one placement even when remote or offsite. The release applicability matrix names the selected engine and version; Restic, Kopia, Borg, Rustic, and other mature engines remain candidates that require separate qualification. Multiple-placement policy remains a later deployment profile.

`RepositoryDriver` requirements:

- Enumerate protected logical paths and snapshots offline.
- Restore by stable logical reference.
- Verify repository and restored output.
- Record the pinned repository reader, repository format, driver profile, configuration requirements, and retrieval instructions in the portable RRF recovery closure. A later profile may additionally package those dependencies in a Capsule Core.
- Export authoritative content to a documented raw or open representation.
- Report whether internal object IDs are stable or implementation-private.

RestoreWeave manifests must not expose an engine pack ID as the permanent logical content identity.

### 2.2 Deferred RestoreWeave-managed packs

In this deferred model, a host-managed-pack `RepositoryDriver` owns or coordinates chunking, compression, pack assembly, encryption, content identities, representation records, placement, readback, verification, reconciliation, restore, and pack-object identities. Any lower-level filesystem, object-store, or swarm transport client is private to that implementation rather than a public RestoreWeave seam.

This model is appropriate for:

- Experimental native storage.
- Trusted swarm storage.
- Portable export.
- Backends that lack a repository engine.

The two models must never be mixed implicitly. Every representation declares its repository ownership model.

## 3. Stable asset, content, representation, and placement identity

The data model must keep three identities separate:

- **Asset ID:** stable user-facing identity across rename, move, remount, and version history when policy considers the item the same logical asset.
- **Content ID:** immutable digest-and-length identity for one exact plaintext byte sequence.
- **Representation ID:** immutable identity for one representation graph node, including its transformation class, output content ID, and dependency/provenance binding.

Namespace reconstruction and physical placement are also separate. A signed namespace record set maps assets and entries to raw and normalized paths, link topology, filesystem metadata, and versioned rename/move/tombstone history; it contains no backend locator. A signed physical placement ledger maps representations and content to engine receipts or RestoreWeave pack locations; it contains no authority over the user-visible namespace.

Backend movement preserves the representation ID; byte-changing source replacement creates a new content ID; a codec or transformation change creates a new representation ID linked through lineage.

Required records:

- Asset, content, and representation IDs.
- Logical size and claim.
- Namespace reconstruction record-set ID and digest.
- Physical placement-ledger ID and digest.
- Backend, repository, object, region, and account.
- Placement generation and state.
- Readback, retention, and failure-domain evidence.

Historical signed manifests reference stable asset, content, and representation IDs plus immutable namespace roots. Append-only namespace generations record user-visible changes, while an independent signed placement ledger maps representations to current and historical physical locations without rewriting old manifests.

## 4. Snapshot commit protocol

Snapshot publication uses:

~~~text
PREPARING
-> STAGED
-> VERIFIED
-> READY_TO_COMMIT
-> COMMITTED
~~~

An attempt may terminate with outcome `ABORTED` before commit, but `ABORTED` is not a published snapshot state and never advances the generation pointer.

Requirements for RW-MVP-1:

- Staged payload data and prepared recovery closure data are invisible to normal restore discovery until a portable commit marker exists.
- `READY_TO_COMMIT` produces an immutable RRF root and a signed `PREPARED_CLOSURE` that bind the exact plan, declared capture or applied-inventory basis, payload snapshot receipt, authenticated-metadata verification evidence, target identity, and publication generation without claiming that publication completed.
- After the prepared closure is stored and reconciled as a `RECOVERY_CLOSURE` placement, RestoreWeave creates a signed `PublicationCommitRecord`. It binds the RRF root, payload receipt, prepared-closure receipt, plan and capture digests, authenticated-metadata evidence, publication generation, and fencing identity.
- The commit record is stored and reconciled as a second small `RECOVERY_CLOSURE` placement. It does not contain its own placement receipt; recovery validates the record bytes and the surrounding repository evidence instead of creating a digest cycle.
- The payload and both recovery-closure roles live inside the same product-level repository placement; they do not constitute redundant or multiple-placement protection.
- A valid portable `PublicationCommitRecord` is the logical commit point. Local SQLite heads, caches, and UI state are rebuildable projections and cannot make a snapshot committed by themselves.
- Clean-machine discovery starts only from valid commit records and ignores orphan payloads or prepared closures. A lost response is reconciled by observing the target and validating the already signed records; it does not create a second logical publication.
- Every generation has an immutable ID, and every crash boundary has a deterministic resume, reconcile, or abort action.

Later distributed-control profiles may add compare-and-swap heads, independent witness services, or richer publication ledgers, but those mechanisms do not redefine the portable RW-MVP-1 commit marker.

## 5. Worker fencing

Leases and heartbeats are insufficient when an old worker resumes after reassignment.

Each attempt receives a monotonically increasing fencing token.

The token is required by:

- Checkpoint writes.
- Storage receipts.
- Verification records.
- Manifest assembly.
- Snapshot publication.
- External side-effect commit.

Expired, cancelled, or superseded attempts may leave cleanable staged data but cannot publish or overwrite newer work.

## 6. Scan generations and source deletion

Only a fully completed scan generation whose `CaptureSetRecord` and every `CaptureRootBindingRecord` pass the publication gate is authoritative. A scanner-local terminal state is necessary but not sufficient.

The system must distinguish:

- Path not observed because the source is offline.
- Permission or parser failure.
- Scan cancelled or incomplete.
- Mount or source identity changed.
- File renamed or moved.
- File deleted at source.
- User request to purge retained recovery data.

Deletion tombstones require comparison between complete generations of the same stable source identity.

Watchers provide incremental hints only. Periodic full reconciliation confirms deletion and coverage. An [fsnotify](https://github.com/fsnotify/fsnotify)-like provider is optional acceleration, not a source of namespace or deletion authority: the public API is non-recursive and requires explicit coverage of every watched directory, and upstream documents that notifications are generally unavailable for NFS, SMB, and FUSE-backed sources. Such sources therefore require polling or another independently qualified provider plus complete reconciliation.

Every watcher or journal hint creates an `IncrementalObservation` containing provider and version, journal epoch and cursor, raw event identity, before and after observations where available, suspected operation, evidence class, and whether a later complete scan confirmed it. The system distinguishes metadata-inferred unchanged content from freshly hash-verified content.

Rename, move, and copy detection produces an `IdentityLinkProposal` unless a deterministic host rule proves the relationship. Equal content, timestamps, path similarity, or reused inode numbers alone cannot merge assets or transfer weaker protection, retention, omission, or deletion authority. A large directory transformation may be grouped into one aggregate event for explanation and anomaly handling while retaining the complete per-entry evidence set.

Source identity must include filesystem or volume identity, mount identity, host identity, and configured root.

### 6.1 Source identity lifecycle

A source is a versioned durable resource rather than a mutable path string. Required source lifecycle states are:

- `ENROLLED`
- `ACTIVE`
- `PAUSED`
- `OFFLINE_EXPECTED`
- `IDENTITY_CONFLICT`
- `REPLACEMENT_PENDING`
- `DECOMMISSIONED`
- `LOST`

Every adopt, replace, relocate, pause, resume, decommission, or lost-source decision creates a signed `SourceIdentityTransitionRecord`. The record binds the previous and successor source IDs where applicable, host, filesystem or volume, mount, configured-root and snapshot-provider evidence, the last complete scan generation, actor, reason, policy epoch, affected selectors and schedules, and whether asset-history continuity was accepted or rejected.

Rules:

- A remount, host rebuild, cloned volume, restored volume, root relocation, or changed filesystem identifier is never adopted silently.
- Adoption or replacement may preserve asset history only after explicit evidence and conflict checks. It cannot transfer a weaker recovery contract, omission approval, source-only treatment, legal scope, or deletion authority automatically.
- Decommissioning stops future scans and schedules but does not delete retained snapshots, tombstones, manifests, or recovery evidence. Retention and deletion remain separate operations.
- A lost source remains visible in protection health and cannot be converted into an intentional decommission without a new signed transition.
- Source lifecycle transitions invalidate affected scan baselines, watcher cursors, identity proposals, plans, and selector-membership approvals until recompiled.

Incremental watcher or journal state is an authenticated `SourceJournalCheckpoint` bound to source identity, provider and version, journal epoch, cursor or token, observed range, declared recursive coverage, watched-directory set or coverage digest, source-filesystem support, and last reconciled complete scan generation. Event loss, queue overflow, reset, truncation, provider restart or change, cursor rollback, missing directory coverage, NFS, SMB, or FUSE use without an independently qualified continuity mechanism, source adoption, identity conflict, mount change, or uncertain continuity invalidates the checkpoint and requires a new complete baseline. Until that baseline commits, watcher events remain hints, coverage is incomplete, and no deletion tombstone may be created.

### 6.2 Change anomaly and preservation hold

RestoreWeave records deterministic source-change anomalies before any retention reduction or destructive lifecycle action. A `ChangeAnomalyRecord` binds the compared complete scan generations, source identity, detector and configuration digest, baseline window, observed counts and bytes, affected selectors, reason codes, severity, evidence, and state.

Required signal families include:

- Unusual mass deletion, rename, move, truncation, or rewrite.
- Sudden extension or file-type distribution changes.
- Abrupt entropy, compression-ratio, content-structure, or executable-signature changes.
- Unexpected canary modification where policy has enrolled canaries.
- Change rate or byte churn outside the version-bound historical envelope.
- Watcher or journal reset combined with substantial unconfirmed change.

LLM output may explain or prioritize an anomaly but cannot create, clear, or lower its safety effect. A qualifying anomaly creates a signed system `ANOMALY_PRESERVATION_HOLD` covering the affected source and the required pre-event history. The hold:

- Never blocks capture or exact preservation of newly observed bytes.
- Blocks source-only promotion, omission, retention reduction, placement retirement, and garbage collection for its scope.
- Preserves the last policy-required known-good generations plus the anomaly window.
- Emits a highest-severity alert and remains effective across restart and control-database rebuild.
- Is released only by a signed resolution that binds the evidence reviewed, actor, reason, retained recovery point, and any follow-up scan or restore drill.

A false positive may delay destructive work but must not cause the newest bytes to be skipped. Absence of an anomaly signal is not proof that source content is benign.

## 7. Application consistency

Every protected entity declares a capture consistency level:

- **CRASH_CONSISTENT:** filesystem snapshot only.
- **APPLICATION_QUIESCED:** application completed a freeze or checkpoint hook.
- **APPLICATION_EXPORTED:** a format-specific export was created and tested.
- **TRANSACTIONALLY_CONSISTENT:** application or database recovery rules were satisfied.
- **OFFLINE_CONSISTENT:** application was shut down before capture.
- **CONSISTENCY_UNVERIFIED:** capture could not prove a valid state.

These entity-level labels refine application recovery evidence and map onto the canonical `CaptureDriver` vocabulary. `CRASH_CONSISTENT` requires a `CRASH_CONSISTENT_VIEW`; `APPLICATION_QUIESCED`, `APPLICATION_EXPORTED`, `TRANSACTIONALLY_CONSISTENT`, and `OFFLINE_CONSISTENT` require an `APPLICATION_CONSISTENT_EXPORT` plus their named evidence; `CONSISTENCY_UNVERIFIED` remains an explicit non-claim. They do not create additional `CaptureDriver` enum values.

Requirements:

- Pre-freeze, snapshot, and post-thaw hooks have timeouts and failure handling.
- Database WAL, journals, transaction logs, schemas, and version compatibility are included where required.
- Multi-volume entities require one consistency boundary or an explicit degraded result.
- VMs and containers require memory or disk-consistency policy.
- Application-specific validators confirm clean open, recovery, migration, and representative queries.
- File hashes alone cannot claim application recovery.

Every authoritative scan and backup begins from one immutable `CaptureSetRecord` created by a qualified `CaptureDriver`. The record contains:

- Capture-set ID, schema version, canonical digest, provider entry-point and package digests, host, source and volume identities, and requested roots.
- One `CaptureRootBindingRecord` per achieved root, binding the trusted root anchor identity, filesystem or volume, mount, root object, requested-to-achieved mapping, resolver profile and kernel facilities, symlink and nested-mount policy, special-file policy, snapshot or validated-live basis, and validation evidence. Runtime descriptor numbers and exposure-path strings are never durable identity.
- Requested and achieved capture-consistency levels.
- Snapshot, barrier, export, or checkpoint identity plus creation, barrier, ready, and declared lease-deadline times.
- Read-only exposure mapping from configured roots to captured roots, cross-volume atomicity and skew, excluded roots, errors, and complete coverage state.
- Fencing token, initial lease owner, permitted consumer scope, provider receipt, and cleanup policy.

The `CaptureSetRecord` is published exactly once only after every achieved root passes the capture publication gate: the retained root anchor, filesystem or volume, mount, root object, snapshot or validated-live basis, resolver policy, nested-mount policy, lease or hold, read-only exposure, and traversal guarantees all validate. A scanner terminal state alone is insufficient, and a run with missing or unchecked boundary evidence cannot publish an authoritative record. `PREPARING` and `FAILED` are capture-attempt outcomes rather than states of an authoritative record. Later binding, lease renewal, consumer completion or abandonment, release request, release success, and release failure are append-only signed `CaptureSetLifecycleEvent` records. Current lifecycle state is derived as `READY`, `IN_USE`, `RELEASE_PENDING`, or `RELEASED` from that event chain. Only `READY` or `IN_USE` may be consumed. A consumer records the exact capture-set ID and canonical digest in every scan observation, engine request, repository receipt, verification record, and candidate manifest.

The capture remains available until every bound consumer reaches a terminal state and its receipt or abandonment event is durable. Cleanup is idempotent and fenced, and its evidence is a later lifecycle event rather than a mutation of the CaptureSetRecord. A live source path cannot be substituted for the captured path while retaining the capture-set identity.

For `RW-MVP-1`, the selected `CaptureDriver` establishes either a retained immutable read-only capture or an honestly declared validated-live capture basis. Inventory, processor reads, hashing, the repository job, and the RRF root bind that same basis or the exact applied-inventory digest produced while streaming admitted bytes.

An atomic snapshot driver may claim `CRASH_CONSISTENT` only when its provider evidence, root mapping, lifecycle, and hold semantics pass qualification. A generic local or NAS-mounted live driver may publish stable admitted files under its declared weaker consistency class, must detect per-entry mutation where promised, and must block or require a new plan for unresolved drift. It never inherits an atomic claim from a platform name.

A qualified ZFS profile creates and inspects snapshots through the version-pinned official [`zfs snapshot`](https://openzfs.github.io/openzfs-docs/man/master/8/zfs-snapshot.8.html) CLI or stable documented machine-readable output, records dataset GUID and snapshot identity, and acquires a [`zfs hold`](https://openzfs.github.io/openzfs-docs/man/master/8/zfs-hold.8.html) before exposing the capture to consumers. The hold and snapshot identity are revalidated before every consumer and are released only through fenced lifecycle cleanup after all consumers terminate. A qualified Btrfs profile uses version-pinned [`btrfs-progs` subvolume](https://btrfs.readthedocs.io/en/latest/btrfs-subvolume.html) CLI output, creates the capture explicitly read-only, and records filesystem UUID, subvolume UUID, parent UUID, generation, received UUID where applicable, and read-only state. Because this profile has no ZFS-equivalent hold, snapshot deletion authority is separated from capture consumers and the identity and read-only state are revalidated before every consumer.

`RW-MVP-1` is file-level. Application-consistency profiles and stronger database, VM, or multi-volume claims require a later compatibility profile. Failure of any optional platform capture integration blocks only that integration profile.

## 8. Filesystem fidelity

Filesystem restoration is separate from content-byte fidelity.

Supported claims:

- **FILESYSTEM_NATIVE_EXACT:** all required source filesystem semantics are restored on a compatible destination.
- **PORTABLE_LOGICAL_EQUIVALENT:** content and a declared portable metadata subset are restored.
- **CONTENT_BYTES_ONLY:** only file content is guaranteed.

The manifest must support:

### Linux and POSIX

- UID, GID, mode, POSIX ACLs, xattrs, capabilities, SELinux labels, immutable flags, sparse extents, hard links, symlinks, devices, FIFOs, and sockets.

### macOS

- Resource forks, Finder metadata, extended attributes, ACLs, file flags, Unicode normalization, APFS clone relationships when required, and package or bundle semantics.

### Windows

- SID-based ACLs, alternate data streams, reparse points, junctions, EFS state, DOS attributes, case behavior, reserved names, and timestamp precision.

### Cross-platform concerns

- Raw path-component representation.
- Case collisions and normalization collisions.
- Path-length and reserved-name limits.
- Hard-link topology.
- Raw symlink target.
- Ownership mapping.
- Timestamp precision loss.
- Destination capability probing.

When a destination cannot represent required semantics, policy chooses:

- BLOCK
- QUARANTINE
- SIDECAR_METADATA
- EXPLICIT_DEGRADE

No degradation is hidden behind ORIGINAL_BIT_EXACT.

## 9. Key lifecycle

The cryptographic design must separate:

- Content-encryption keys.
- Manifest-encryption keys.
- Manifest-signing keys.
- Approval-signing keys.
- Backend credentials.

Required key records:

- Key ID and purpose.
- Algorithm and parameters.
- Recipient or custodian.
- Wrapping key and KDF.
- Creation, activation, rotation, expiry, and revocation.
- Historical generations that depend on the key.
- Recovery and compromise procedure.

Requirements:

- Support multiple recipients.
- Permit optional escrow or threshold recovery.
- Preserve offline recovery when KMS, IdP, cloud accounts, hardware tokens, and secret managers are unavailable.
- Rewrap keys without rewriting payloads only for recipient or KEK changes when the affected DEKs were not disclosed.
- Test every retained generation before and after rotation.
- Preserve old uncompromised decryption keys until zero retained dependents remain. A compromised DEK follows the verified re-encryption migration below and cannot remain a healthy confidentiality path merely because dependents still reference it.
- Define algorithm-agility migration.
- Resolve conflicts among crypto-erasure, retention, and legal hold.
- Define a durable nonce-allocation or deterministic derivation invariant that remains unique across retries, process crashes, fencing changes, migrations, and re-encryption generations.

### 9.1 KeyRecoveryPolicy and ceremonies

Every key or credential required for offline recovery is covered by an immutable signed `KeyRecoveryPolicy`. This includes RestoreWeave encryption and signing roots, approval and recovery-authority roots, each repository password or repository-key bootstrap, backend endpoint and account-recovery dependencies, SFTP host-authentication material where applicable, and any key needed to authenticate the Recovery Bootstrap Seed, RecoveryHeadWitness, Recovery Bootstrap Envelope, manifest, or placement ledger.

The policy records:

- Policy ID, revision, canonical digest, protected key or credential purposes, and dependent generations or repositories.
- Recipient and custodian identities, threshold, share or wrapped-recipient identifiers, independence groups, permitted recovery environments, and geographic or organizational constraints.
- Out-of-band root fingerprints, sealed-media or hardware-token references, share format and version, expiry, refresh interval, and successor policy.
- Required creation, distribution, acknowledgement, recovery-test, rotation, compromise, replacement, revocation, zeroization, and evidence-retention procedures.
- Maximum tolerated lost, unavailable, or compromised custodians and the resulting health state.

Raw shares, passwords, private keys, recovery codes, and reconstructed secrets never appear in manifests, the ordinary control database, API responses, logs, diagnostic bundles, prompts, or ceremony evidence. No ordinary source host, worker, storage credential, or single control-plane principal may hold enough material to satisfy a threshold policy by itself.

A signed `KeyRecoveryCeremonyRecord` binds the exact policy revision, ceremony type, participating custodian and share IDs, authenticated environment, start and completion time, threshold result, recovered-key fingerprint or credential-validation result, repositories and generations tested, cleanup and zeroization evidence, exceptions, and outcome. It proves that recovery succeeded or failed without exposing recovered material.

Required lifecycle events include `CREATED`, `DISTRIBUTED`, `ACKNOWLEDGED`, `TESTED`, `SHARE_LOST`, `SHARE_COMPROMISED`, `CUSTODIAN_REPLACED`, `SHARES_REFRESHED`, `SUPERSEDED`, and `REVOKED`. Custodian replacement or threshold-share refresh creates a new policy or share generation and never rewrites prior evidence. A compromised share triggers policy-defined refresh or root rotation; a lost share immediately changes quorum health.

Protection health distinguishes `KEY_RECOVERY_HEALTHY`, `KEY_RECOVERY_AT_RISK`, and `KEY_RECOVERY_BLOCKED`. A policy is healthy only when its current independent threshold can be assembled within the declared RTO and a clean recovery ceremony is within cadence. Losing quorum blocks any claim of complete offline recovery even when ciphertext replicas remain healthy.

`RW-MVP-1` remains single-workspace and single-administrator. Clean recovery requires an independently retained recovery reference, a scoped repository credential source, and an independent trust anchor; none may be accepted merely because it was supplied by the companion being authenticated. Threshold custodians, escrow ceremonies, and multiple repository credentials are later profiles.

Compromise handling must distinguish:

- **Wrapping-key or KEK compromise without DEK disclosure:** rewrap affected DEKs under a new trusted recipient or KEK, rotate trust metadata, and verify every dependent generation.
- **Content-encryption key or DEK compromise:** rewrapping is insufficient. Decode and verify the authoritative plaintext, encrypt it under a new DEK and new nonce domain, create and independently read back new placements, publish a new signed placement generation, then retire or crypto-erase the compromised key and ciphertext generation only when retention and legal-hold policy permits.

Ciphertext already published to a public or uncontrolled swarm cannot be recalled. It remains a compromised placement after DEK disclosure and must not count toward confidentiality or healthy-replica policy.

## 10. Deferred legacy Capsule Core and recovery-bootstrap model

> **Superseded for RW-MVP-1:** The model in this section records an earlier enterprise recovery design. The current MVP uses a portable RRF companion closure plus a signed `PublicationCommitRecord`, recovered with a scoped credential source and independent trust anchor. The records below may inform a later named custody or distributed-control profile, but they are neither emitted nor required by the current product.

The deferred model uses an acyclic layered corpus so the first-stage reader never depends on the Capsule Core it is intended to load.

### 10.1 Capsule Core

A content-addressed **Capsule Core** contains snapshot-independent recovery tooling and dependency closure:

- Manifest readers for every referenced schema version.
- Minimal restore orchestrator.
- Decryption and signature verification tools.
- Backup-engine readers.
- Required `Processor.TRANSFORM` and `Processor.VALIDATE` implementations, later `RetrieverDriver` protocol implementations, and core-owned restore tooling.
- Models, tokenizers, dictionaries, delta bases, and configuration.
- Portable CPU binaries for supported architectures.
- A second execution form such as WASM, OCI layers plus a runnable engine, or a VM appliance.
- Source, locked dependencies, toolchain, and reproducible-build instructions.
- SBOM, licenses, notices, conformance vectors, and expected hashes.
- Human-readable and printable recovery instructions.
- Storage-target discovery and credential-recovery procedure.
- A proof that no network fetch is required for the declared offline recovery path.

The core does not embed or point back to the snapshot manifest that references it. A manifest may safely list required core digests because this direction is acyclic.

### 10.2 ControlPlaneRecoverySet

A signed, content-addressed **ControlPlaneRecoverySet** protects durable control-plane facts that snapshot manifests cannot reconstruct alone. It contains or authenticates independently stored point-in-time catalog and event-log state, published policies and recovery contracts, annotation schemas, user annotations and corrections, claim resolutions, processor-profile publications, AutomationGrants, approvals and revocations, configuration, schedules, service objectives, notification state, audit history, rights and hold records, source transitions, CaptureSetRecords, key-recovery evidence, worker-enrollment descriptors, and fencing or reconciliation watermarks.

Each generation binds its predecessor, publication domain, consistency point, RPO and RTO, ordered component digests, schema and reader Capsule Core digests, encryption and key-recovery references, ACL and residency policy, signing quorum, `RecoveryArtifactPlacement` receipts, and independent full-readback evidence. It never contains raw credentials or plaintext keys and never revives stale jobs, leases, cached approvals, or fencing authority. Search indexes and disposable enrichment are rebuilt instead of protected as authoritative state.

### 10.3 Recovery Bootstrap Envelope

After snapshot publication, RestoreWeave creates a separately signed **Recovery Bootstrap Envelope** that points one way to:

- The committed `SnapshotPublicationRecord` and candidate-manifest digest.
- The signed placement-ledger checkpoint.
- The exact ControlPlaneRecoverySet generation, digest, reader requirements, and protected placements.
- Required Capsule Core digests and their offline placements.
- The authenticated key-bootstrap and wrapped-key recovery references.
- Rollback and fork-detection information.

The publication record and manifest do not reference the envelope that names them. Envelopes are replicated to independently discoverable immutable recovery locations and may form their own signed newest-to-parent chain.

Every ControlPlaneRecoverySet and Capsule Core required by the declared offline path must be embedded in the envelope bundle or present at a verified offline-capable `RecoveryArtifactPlacement`. An externally reacquired-only core may support an optional online path but cannot satisfy minimum offline recovery closure.

Before retiring any placement or representation referenced by a retained Recovery Bootstrap Envelope, Recovery Bootstrap Seed, ControlPlaneRecoverySet, Capsule Core, decoder, or validator, RestoreWeave must publish, independently replicate, and cold-verify the complete successor set for the same committed snapshot: physical-placement checkpoint, Recovery Bootstrap Envelope, RecoveryHeadWitness, Recovery Bootstrap Seed, BootstrapSeedSuccessorRecord, ControlPlaneRecoverySet when changed, and required RecoveryArtifactPlacement records. The old path remains a recovery and payload-liveness root until successor discovery, signatures, witness freshness, key bootstrap, control-plane recovery, Capsule Core resolution, and a post-publication bootstrap drill pass without relying on the location being retired.

Capsule Cores, ControlPlaneRecoverySets, Recovery Bootstrap Envelopes, Recovery Bootstrap Seeds, BootstrapSeedSuccessorRecords, and RecoveryHeadWitness records and transitions require independent immutable replicas and periodic clean-hardware boot and post-publication bootstrap drills.

### 10.4 Recovery Bootstrap Seed and RecoveryHeadWitness

At least one independently replicated **Recovery Bootstrap Seed** uses a documented baseline format readable without a RestoreWeave repository, plugin registry, network service, or the Capsule Core it is intended to load. It contains exact signed envelope and current RecoveryHeadWitness copies, pinned trust anchors, canonical signature material, the minimum manifest and placement reader, extraction instructions, and any first-stage executable or source required to load the initial core. Its signature binds publication domain, publication-record digest, generation, predecessor-Seed digest, exact Envelope and Witness digests, inventory, and bootstrap-root policy. Required key-unwrapping material is available out of band and does not depend on that core.

After each snapshot publication, receipt reconciliation, protected ControlPlaneRecoverySet generation change, envelope replacement, witness-epoch transition, or authoritative physical-placement checkpoint event affecting discovery, eligibility, health, supersession, retention, quarantine, or retirement, RestoreWeave publishes a successor Envelope whenever any bound digest changes, then a monotonic signed **RecoveryHeadWitness** to a policy-defined independent witness set, then a successor Seed containing that exact Envelope and Witness. The witness binds publication, manifest, receipt, envelope, placement checkpoint, predecessor, generation, epoch, and observation time. Persisted highest-seen witness state detects rollback; conflicting valid successors produce a blocked fork state until a separately signed resolution record is published. The Seed is not complete until independent replica receipts, cold verification, and a signed BootstrapSeedSuccessorRecord bind its predecessor and successor lineage.

Before retiring a Seed, Envelope, Witness, ControlPlaneRecoverySet, Core, or recovery-artifact placement, publish and cold-verify the complete successor set from only the declared baseline platform assumptions and out-of-band recovery material.

### 10.5 Bootstrap completion and artifact lifecycle

Bootstrap completion is a derived state machine:

~~~text
BOOTSTRAP_PENDING
-> ENVELOPED
-> WITNESSED
-> SEEDED
-> REPLICATED
-> COLD_VERIFIED
~~~

Atomic publication, every protected ControlPlaneRecoverySet generation change, and every later authoritative checkpoint change begin at `BOOTSTRAP_PENDING`. Each transition requires the exact signed evidence named by the Restore Manifest. `COMMITTED` does not imply `COLD_VERIFIED`. A generation below `COLD_VERIFIED` is not protection-healthy and cannot authorize source-only omission, retention reduction, placement or bootstrap-artifact retirement, garbage collection, deletion, or destructive destination overwrite. Preservation-only capture, replication, reconciliation, and bootstrap completion remain permitted.

Every `RecoveryArtifactPlacement` uses `ACTIVE`, `GRACE`, `QUARANTINED`, `RETAINED_EVIDENCE`, or `RETIRED`. Active, grace, and quarantined placements remain payload-liveness roots. Evidence-only placement retains the artifact bytes or a separately qualified compact validation anchor required to authenticate history without pinning superseded payload placements. Retirement preserves signed placement history and requires a complete `COLD_VERIFIED` successor.

## 11. Codec, plugin, model, and protocol lifecycle

Required lifecycle:

~~~text
EXPERIMENTAL_DUAL_WRITE
-> QUALIFIED
-> DEPRECATED_WRITE_BLOCKED
-> MIGRATION_REQUIRED
-> READ_ONLY_LEGACY
-> RETIRED_NO_DEPENDENTS
~~~

Any state may become QUARANTINED after a correctness, security, license, or supply-chain failure.

The lifecycle monitor tracks:

- Dependent snapshots, assets, and bytes.
- Conventional fallback coverage.
- Last online and offline decode success.
- Supported operating systems and CPU architectures.
- Reproducible-build and artifact availability.
- License or redistribution changes.
- Vulnerabilities and abandonment.
- Accelerator, registry, account, certificate, and network dependencies.
- Restore-kit and manifest-reader compatibility.
- Migration cost, urgency, and blast radius.

### Qualification

Before an experimental exact codec can retire its fallback:

1. Redistribution rights are confirmed.
2. A CPU-only deterministic reference decoder exists behind the pinned `Processor.DECODE_REPRESENTATION` operation and remains qualified for every retained dependent representation even after it stops accepting new encoding work.
3. The Recovery Bootstrap Seed, BootstrapSeedSuccessorRecord, current RecoveryHeadWitness, Recovery Bootstrap Envelope, ControlPlaneRecoverySet, RecoveryArtifactPlacements, and retained Capsule Cores restore without DNS, registries, package managers, P2P, or accelerators.
4. Full round trips pass on at least two independent environments.
5. Migration back to raw or Zstandard succeeds.
6. The configured observation window and repeated cold drills pass.
7. A signed risk approval and grace period complete.

Round-trip qualification gives the decoder only the sealed candidate representation and pinned dependencies, never the original source handle. The host independently computes decoded length and digest. Range, seek, restart, streaming, temporary-space, and minimum-readable-unit claims are qualified against the same `FileAccess` path used by browse, FUSE, verification, migration, and restore.

Neural arithmetic codecs must preserve integer-CDF rules, framing, precision, endian behavior, preprocessing, model code, weights, tokenizer, runtime, and conformance vectors.

## 12. Representation migration

Migration uses:

~~~text
enumerate dependents
-> decode and verify old representation
-> encode and verify target representation
-> replicate and independently read back
-> publish a new immutable manifest generation
-> publish Envelope and Witness, create and replicate successor Seed
-> post-publication bootstrap drill to COLD_VERIFIED
-> grace period
-> release old representation only after zero retained references
~~~

Requirements:

- Historical manifests are not rewritten.
- New records link through supersedes.
- Migration is resumable and fenced.
- Cancellation leaves the old authoritative representation intact.
- Lossy media migrates from the strongest retained source, not through repeated lossy generations.
- Migration receipts record old and new identities, environments, claims, and verification.
- Every codec, decoder, model, validator, reader, or Capsule Core migration instance that affects a retained publication must produce and cold-verify the complete successor checkpoint, Envelope, RecoveryHeadWitness, Seed, Seed-successor record, ControlPlaneRecoverySet binding, RecoveryArtifactPlacement set, and dependency closure before retiring the old representation or runtime.
- A general release-qualification result does not replace the per-publication and per-migration-instance successor gate.

## 13. Backend lifecycle and migration

Backend states:

- ACTIVE
- READ_ONLY
- DRAINING
- RETIRED
- LOST
- QUARANTINED

Migration:

~~~text
copy
-> independent readback
-> coverage and failure-domain proof
-> commit new placement
-> grace period
-> retire old placement
~~~

Replica requirements must not drop during migration. Object-lock, rekey, retention, cost, capacity, and rollback behavior must be declared.

If a retained Recovery Bootstrap Envelope, Recovery Bootstrap Seed, ControlPlaneRecoverySet, Capsule Core, decoder, or validator references the retiring backend, placement generation, checkpoint, or envelope, migration is incomplete until the successor physical-placement checkpoint, Recovery Bootstrap Envelope, RecoveryHeadWitness, Recovery Bootstrap Seed, BootstrapSeedSuccessorRecord, and required RecoveryArtifactPlacements for the same committed snapshot have been published, independently replicated, and reached `COLD_VERIFIED` as one lineage. Historical manifests and publication records remain immutable.

## 14. Retention, omission, and garbage collection

Original-strength reduction uses:

~~~text
STRONGLY_PROTECTED
-> ELIGIBLE
-> APPROVED
-> GRACE
-> DELETE_PENDING
-> DELETE_VERIFIED
~~~

Approval may be revoked during the grace period.

Garbage collection is two phase:

~~~text
mark and report
-> approval and grace
-> fenced sweep
-> backend deletion confirmation
~~~

Deletion authority is ownership-mode specific:

- **ENGINE_MANAGED_OBJECTS:** RestoreWeave computes snapshot- and representation-level retention intent, submits a fenced engine snapshot deletion or retention request, and audits the engine's repository check and receipt. RestoreWeave never sweeps engine-private chunks or packs directly.
- **RESTOREWEAVE_PACKS:** RestoreWeave computes physical reachability from authenticated roots and applies the fenced object sweep itself through the applicable `RepositoryDriver` capability.

GC traverses typed `PAYLOAD_LIVENESS` and `AUTHENTICATION_EVIDENCE` edges. Retaining signed evidence does not automatically retain every historical payload location, and retiring a payload placement never makes its authentication history disappear.

Payload-liveness roots include:

- All retained snapshot publication records and signed candidate manifests.
- Legal holds.
- Active anomaly-preservation holds, their protected pre-event baselines, anomaly windows, and unresolved resolution jobs.
- Every nonterminal backup, upload, scan-commit, restore, repair, revalidation, scrub, verification, migration, rekey, capsule-build, and reconciliation job.
- Resumable cancellation checkpoints, staged commit generations, multipart uploads, partial torrent data, and unexpired worker attempts.
- Isolated recovery generations.
- Authoritative and grace-period representations.
- Decoder, model, dictionary, base, validator, and reduced-reference dependency closure.
- RecoveryArtifactPlacements in `ACTIVE`, `GRACE`, or `QUARANTINED`, including their required Recovery Bootstrap Envelopes, Recovery Bootstrap Seeds, ControlPlaneRecoverySets, RecoveryHeadWitness records, and Capsule Cores.

Authentication-evidence roots include:

- Signed RecoveryArtifactPlacement creation, transition, supersession, and retirement history.
- Persisted highest-seen witness bytes or immutable references, witness-set policies, signer and key-version history, BootstrapSeedSuccessorRecords, and required predecessor chains or compact validation anchors.
- WitnessEpochTransitionRecords, PublicationForkResolutionRecords, every raw observed fork or rollback evidence digest, and every quarantined branch until a separate signed disposition is valid after the new witness epoch.
- ControlPlaneRecoverySet inventories and signatures, Envelope and Seed lineage, cold-verification evidence, and post-publication bootstrap-drill results needed to validate retained publications or retirement decisions.

Backend listing alone is not authoritative when consistency is eventual. GC must use signed publication/manifest roots and placement or engine receipts.

Staged objects, multipart uploads, partial torrent pieces, cancelled-but-resumable outputs, and committed authoritative data have distinct cleanup policies.

User-facing lifecycle actions remain distinct:

- `CATALOG_HIDE` changes an ordinary view only.
- `RESTORE_SUPPRESSION` changes ordinary restore presentation while retaining governed evidence.
- `SOURCE_TOMBSTONE` records absence confirmed by complete scan generations.
- `SOURCE_DELETE` intentionally mutates a live source only through a versioned SourceDeletePlan and durable job bound to the exact source revision, selector expansion, protected preimage, current policy, action-specific approval, destination mutation journal, verification, and reconciliation. It is prohibited in `RW-MVP-1`.
- `STOP_PROTECTION` stops future protection after policy review.
- `SNAPSHOT_RETIREMENT` removes one snapshot from the retained policy root set.
- `PHYSICAL_GC` deletes collectible objects after mark, report, approval, grace, fencing, and receipt.
- `PRIVACY_PURGE` applies its own lineage, legal-hold, audit, crypto-erasure, and external-recall semantics.

No API or UI control may collapse these operations into one ambiguous Delete action.

Every replication target declares deletion coupling as `INDEPENDENT`, `MIRROR_WITH_FLOOR`, or `NEVER_DELETE`. Its `RepositoryDriver` exposes a dry-run or exact candidate set for retention changes and reports capability drift. A source-side retention policy never implies deletion of the last healthy independent remote recovery point.

## 15. Later-profile scheduling

RW-MVP-1 Core owns no scheduler or resident daemon. Saved-profile cadence and health expectations belong to reference userland, while a later scheduling profile may record:

- Schedule ID.
- UTC due time and user timezone.
- DST behavior.
- Jitter.
- Missed-run catch-up or coalescing.
- Maximum overlap.
- Maintenance window.
- Retry, age, cost, and attempt limits.
- Dead-letter or quarantine policy.
- Governing RPO deadline and priority.
- Source-mount, reconnect, and change-trigger policy.
- Prerequisite jobs and evidence.
- Preemption and restore-reservation behavior.
- Explicit behavior when a removable source remains absent.

The pair of schedule ID and due time is unique.

Source degradation, promotion, key tests, and restore drills have higher priority than enrichment.

Jobs re-evaluate policy, plugin, credential, immutable rights-evidence, signed rights-determination, action-specific operational-approval, network-profile, and input versions before resuming a checkpoint and before every network, filesystem-publication, deletion, upload, seeding, migration-retirement, GC, or other external commit.

Authority records carry a signed revocation epoch, trusted-time evidence, maximum freshness age, and validity window. Workers use monotonic deadlines for an active attempt, detect wall-clock rollback against the last authenticated time checkpoint, and refresh authority within a bounded revocation-propagation SLO. If trusted time or revocation freshness is unavailable or too old, preservation-only work may remain staged, but network, omission, destructive, and canonical-publication actions fail closed.

## 16. Resource-aware execution

Policies may constrain:

- Metered or roaming networks.
- Battery level and charging status.
- Thermal state.
- User-active versus idle time.
- CPU, GPU, RAM, disk, inode, and spool pressure.
- Upload, download, and egress budgets.
- Per-target concurrency.
- Restore-priority reservations.

Capacity exhaustion fails open for preservation and fails closed for omission or deletion.

## 17. Later-profile control-plane recovery

`RW-MVP-1` treats the embedded catalog, baseline search index, and other local operational projections as rebuildable and proves clean restore directly from the committed portable RRF closure. MVP whole-subject tag and plain-text note records are already durable user data: authenticated portable annotation bundles preserve their revisions independently of SQLite, indexes, and the deferred control-plane recovery machinery. The broader managed-control-plane recovery model below is deferred.

Under a later profile, richer annotation state and the control database, queue, policy, approvals, and audit history may become durable control-plane data protected by that broader model. MVP tag and note durability does not depend on activating it.

Requirements:

- Point-in-time recovery and declared RPO/RTO.
- Independent backup of catalog, event log, and configuration.
- A signed ControlPlaneRecoverySet for every protected control-plane recovery point, with predecessor, component inventory, consistency point, schemas, reader Cores, key-recovery references, ACL and residency policy, independent RecoveryArtifactPlacements, and full-readback evidence.
- Ability to begin from a Recovery Bootstrap Seed and current RecoveryHeadWitness, authenticate the exact Recovery Bootstrap Envelope and its ControlPlaneRecoverySet, then rebuild the representation graph from signed snapshot publication records, candidate manifests, receipts, placement ledgers, and Capsule Cores.
- Rebuild source identities and transitions, CaptureSetRecords, anomaly-preservation holds, KeyRecoveryPolicies and ceremony evidence, service-SLO definitions, and notification state from authenticated records or explicitly mark unavailable operational projections.
- Defined authority when catalog, manifest, and backend disagree.
- Fencing and reconciliation of active jobs after database loss.
- Secure rebuild of search indexes and ACL filters.
- Re-enrollment of workers and credential references.

Signed publication records, their authenticated candidate manifests, source-identity transitions, CaptureSetRecords, anomaly-preservation holds, KeyRecoveryPolicies, placement ledgers, ControlPlaneRecoverySets, Recovery Bootstrap Envelopes, Recovery Bootstrap Seeds, BootstrapSeedSuccessorRecords, RecoveryHeadWitness history, RecoveryArtifactPlacements, Capsule Cores, and authenticated receipts are authoritative for their respective recovery decisions. The catalog is an operational index that must be reconstructible.

## 18. Restore destination semantics

Preflight must check:

- For RW-MVP-1, the exact target descriptor, scoped credential source, independently retained recovery reference or exact selector, independent trust anchor, RRF root, payload receipt, prepared-closure receipt, and valid `PublicationCommitRecord`.
- For a later profile that activates the legacy enterprise-bootstrap design, its exact Seed, Envelope, Witness, control-plane, placement, and reader tuple.
- Capacity, quota, temporary space, and sparse-file expansion.
- Privileges and filesystem capabilities.
- Case, normalization, reserved-name, and path-length collisions.
- Existing-path behavior: block, overwrite, merge, rename, skip, or atomic replace.
- Destination modification during restore.
- Malware and quarantine policy.
- Egress cost and estimated time.

Restore is a destination transaction:

1. Build an immutable namespace and mutation plan.
2. Stage bytes and metadata in an isolated restore root, preferably on the destination filesystem.
3. Validate content, component, filesystem, and application outcomes before canonical publication.
4. Publish through an atomic directory/subvolume swap or per-entry atomic replace where supported.
5. Persist a write-ahead destination mutation journal before every non-atomic overwrite, merge, rename, metadata change, or deletion.
6. On crash or cancellation, reconcile the journal and either resume, roll back from retained preimages, or leave an explicit isolated partial result.

If the destination cannot support atomic publication or retained preimages, overwrite and merge require explicit non-atomic-risk acceptance; the safe default is a separate restore root. A job cannot report success while journal entries are unresolved or the destination changed outside the plan.

Restore supports partial selection, priority ordering, cancellation, resume, and deterministic reconciliation or rollback according to the recorded destination capability.

Immediately before the first destination mutation and before every resume, rollback, reconciliation, overwrite, merge, or atomic publication, the worker re-observes and validates the exact recovery tuple required by the selected profile. For RW-MVP-1, target, credential scope, trust anchor, RRF root, payload, prepared closure, and portable commit must still agree. A later witness-enabled profile additionally validates its highest-seen state. Tuple drift, invalid signatures, broken lineage, rollback evidence, or unresolved freshness state stales the preflight and blocks mutation; a separately qualified later profile may permit only non-destructive extraction to a new empty destination.

Required states include:

- BLOCKED_PATH_COLLISION
- BLOCKED_CAPACITY
- BLOCKED_PRIVILEGE
- DEGRADED_FILESYSTEM_METADATA
- APPLICATION_CONSISTENCY_UNVERIFIED
- RESTORE_CANCELLED
- RESTORE_PARTIAL
- RESTORE_ROLLBACK_REQUIRED
- RESTORE_DESTINATION_DIVERGED

## 19. Replica independence

Failure independence is modeled across:

- Provider.
- Account and billing domain.
- Region, site, and physical medium.
- Operator principal.
- Delete credential.
- Recovery key and custodian.
- Repository format and reader.
- Network path.

Two buckets in one account or two locations using one lost key are not independent replicas.

## 20. Schema and rolling upgrades

Requirements:

- Compatibility matrix for control server, worker, plugin, database, checkpoint, API, and manifest versions.
- Mixed-version worker rules.
- Crash-safe resumable database migration.
- Preflight backup and free-space checks.
- Roll-forward and rollback strategy.
- Immutable historical manifests plus signed migration attestations.
- Must-understand extension handling and golden fixtures.
- Reader support for every schema referenced by retained snapshots.
- Explicit BLOCKED_UNSUPPORTED_SCHEMA or BLOCKED_UNSUPPORTED_PROTOCOL_VERSION results.

## 21. Later-profile remote API operations

RW-MVP-1 exposes CLI and a qualified read-only MCP adapter over the Core Command ABI; it does not require REST. If a later remote or multi-user API profile is supported, it must define:

- Structured errors with stable codes.
- Pagination, sorting, filtering, and projection.
- Rate limits, quotas, Retry-After, and principal budgets.
- Idempotency-key scope, retention, and payload-mismatch behavior.
- Event cursors, replay, retention, and schema versions.
- Signed webhooks and delivery history.
- Tenant boundaries and RBAC or ABAC evaluation.
- Token revocation and credential rotation.
- Request, trace, causation, and correlation IDs.
- OpenAPI compatibility and deprecation policy.
- Worker enrollment, attestation, draining, and capability updates.
- Plugin install, signature, quarantine, rollback, and dependency resolution.

## 22. Policy and approval lifecycle

Policy states:

- DRAFT
- PUBLISHED
- ACTIVE
- SUPERSEDED
- ROLLED_BACK
- REVOKED

Policies compose deterministically from system safety floor through workspace, dataset, entity, asset, and component scope. Safety floors, legal holds, and explicit denies override ordinary weaker rules. Equal-specificity conflict without a published tie-break blocks weakening and preserves exact bytes. Every effective-policy record binds selector membership, field-level rule provenance, compiler version, approvals, and explanation.

Bulk approvals:

- Resolve and display current member count, bytes, samples, and risk distribution.
- Bind the current snapshot's concrete members by default.
- Require separate authority for future dynamic matches.
- Support stronger confirmation or dual approval for high-risk actions.
- Provide grace-period revoke and undo.
- Show affected objects when approval becomes stale.

An `AutomationGrant` applies the same immutable scope, revision, signature, revocation, freshness, and dynamic-membership discipline to repeatable low-risk automation. It may authorize scanning, exact preservation, verification, index rebuild, recommendations, or adding a policy-compliant replica. It never replaces the action-specific approval for omission, deletion, retention reduction, metadata writeback, external upload or publication, weaker restore acceptance, identity merge or split, key action, or last-copy retirement.

Approval revocation is a new signed record, not an in-place rewrite. Every active job observes the latest applicable revocation epoch before an external or destructive commit; application restart never revives authority from a stale cached approval.

Roles should distinguish viewer, operator, policy author, omission approver, restore approver, key custodian, auditor, and administrator.

Legal holds are immutable signed resources with authority, scope, jurisdiction, reason, creation, review, expiry or indefinite status, and a separately signed release event. A hold overrides retention, garbage collection, source omission, placement retirement, privacy deletion, and crypto-erasure for its scope.

Privacy deletion distinguishes catalog purge, derivative and index purge, restore suppression, credential removal, crypto-erasure, backend deletion, and externally unrecoverable copies. Immutable integrity records minimize sensitive data and may retain signed tombstones or blinded identifiers where complete removal would break authenticated history.

## 23. User experience and alerts

### 23.1 Service objectives, telemetry, and independent monitoring

Product service objectives are separate from a dataset's `ProtectionObjective`. Every supported deployment publishes versioned `ServiceLevelObjectiveRecord` values, measurement windows, exclusions, owners, and escalation rules for at least:

- Schedule and RPO-breach detection latency.
- Age of the latest complete scan, immutable capture set, committed generation, independent repository verification, and clean restore drill.
- Job queue delay, stalled-attempt detection, worker heartbeat age, and reconciliation backlog.
- Repository, key-recovery quorum, Recovery Bootstrap Seed, RecoveryHeadWitness, Recovery Bootstrap Envelope, Capsule Core, and placement-health observation age.
- Notification enqueue, first delivery, retry, escalation, acknowledgement, and resolution latency.
- Control API availability where the API is part of the supported profile.
- Telemetry ingestion delay, dropped-event rate, backlog, storage saturation, and diagnostic-bundle generation.

Each SLI defines its numerator, denominator, clock and trusted-time source, aggregation, missing-data behavior, and machine-readable reason codes. Missing telemetry, an unknown denominator, or a stopped monitor is never interpreted as healthy.

A later managed-service notification profile may use an independent dead-man monitor outside the protected source host and ordinary control database. The source periodically publishes a minimal authenticated health watermark containing a pseudonymous workspace or monitor ID, policy epoch, latest committed publication generation and time, latest successful independent verification time, next expected heartbeat, and status digest. It contains no paths, filenames, content hashes, secrets, repository locators, or key material. This monitor is not an RW-MVP-1 requirement.

The independent monitor alerts when a watermark is late, invalid, rolled back, or stops advancing. It cannot approve policy, establish recovery correctness, decrypt content, or replace a restore drill. If independent monitoring is not configured, the deployment reports `AT_RISK_NO_INDEPENDENT_MONITOR` and cannot claim the production notification profile.

Metrics, traces, events, and logs declare sensitivity, access control, retention, aggregation, cardinality limits, redaction, export destinations, and behavior under backpressure. Telemetry loss must remain visible and must not block preservation work; it fails closed for claims that require fresh evidence.

### 23.2 User experience and alerts

RW-MVP-1 first-run onboarding verifies:

- Source coverage, read permissions, mount boundaries, achieved capture consistency, mutation-detection capability, and qualified `CaptureDriver` behavior. Optional privileged snapshot helpers are checked only when selected.
- One mature exact repository target and its deduplication, compression, encryption, readback, and reconciliation capabilities; same-failure-domain use is limited to explicit test mode and a separate machine, remote, or otherwise separate-failure-domain target is recommended.
- Suffix and magic identification, default metadata/text processor health and sandbox limits, generic exact fallback, baseline index creation, and search-generation status.
- A scoped credential reference without embedding the credential in a plan or recovery record.
- An independent trust anchor and a destination for the exported recovery reference.
- One exact committed generation, current sampled verification, and the path to a clean restore drill.

Offline media, bootstrap Seed or Capsule artifacts, key quorums, dead-man registration, privacy or AI egress, and multi-placement topology are onboarding checks only for later profiles that actually support them.

The main status separates:

- Exact protected.
- Conditionally recoverable.
- Pending verification.
- At risk.
- Unscanned.
- Unrecoverable.

Highest-severity alerts include:

- Local original disappeared before promotion.
- Source identity conflict, journal invalidation, failed selected capture, unresolved live-source drift, or active change anomaly.
- Replica or immutability below policy.
- Key, Recovery Bootstrap Seed, RecoveryHeadWitness, Recovery Bootstrap Envelope, or Capsule Core restore failed.
- Source, decoder, validator, or entitlement degraded.
- Snapshot schedule or RPO breached.
- GC or deletion waiting for approval.
- Control database or placement ledger divergence.
- Lost or compromised key share, missing recovery quorum, stale recovery ceremony, late or invalid health watermark, and notification-delivery SLO breach.

The graph editor is an advanced mode; safe presets and guided workflows are the default.

## 24. Manifest scale and interoperability

For millions of entries:

- Use deterministic content-addressed shards.
- Sign or Merkle-root the shard index.
- Support bounded-memory streaming validation.
- Support partial restore lookup.
- Isolate and repair damaged shards.
- Preserve complete dependency closure after parent pruning.

Interoperability requirements:

- Public schemas.
- Standalone validator and reference reader.
- Raw bytes plus filesystem-metadata sidecar export.
- BagIt, OCFL, or Metalink-compatible profiles where useful.
- Independent implementation conformance tests.
- Full escape path from an abandoned backup engine or RestoreWeave implementation.

### 24.1 RWPORT-1 portable recovery package

The first normative export profile is `RWPORT-1`, represented as a directory tree and optionally transported as a deterministic POSIX tar archive. Compression is an outer optional transport layer and is not required to discover or validate the package.

Required layout:

~~~text
rwport.json
bootstrap/
publications/
manifests/
placements/
records/
objects/
filesystem-sidecars/
signatures/
checksums/
licenses/
reports/
~~~

`rwport.json` binds profile version, package ID, creation tool and digest, selected snapshots and components, completeness mode, encryption profile, canonical record-set root, checksum algorithms, signature set, required readers, and explicit loss report.

Rules:

- Records retain their original canonical bytes and signatures.
- Payload objects may be embedded, encrypted, or represented by authenticated external placement references according to the export contract.
- Raw file bytes and filesystem metadata sidecars are distinct.
- Partial export lists every omitted component and cannot claim complete recovery.
- Secret-bearing material is encrypted or excluded with an explicit blocked dependency.
- Package validation is streamable and bounded-memory.
- A sealed `RWPORT-1` package may use a swarm-capable `RepositoryDriver` for an additional transport placement and a separately qualified later `RetrieverDriver` for reacquisition. Torrent metainfo and locators are created after sealing and recorded outside the package digest; at least one independently verified non-P2P offline-capable placement remains mandatory, and the swarm never supplies signing or freshness authority.
- A future `RWPORT-1` package-verifier command validates structure, signatures, checksums, dependency closure, and loss report without writing a restore destination. Its CLI spelling is not part of the RW-MVP-1 command contract.
- An independent implementation must be able to validate `RWPORT-1` from the public schema and conformance vectors.

## 25. Search and semantic lifecycle

`RW-MVP-1` owns the subject bindings and generation truth for rebuildable local lexical, structured, and semantic indexes over path, metadata, type, checksum, duplicate, common-media metadata, durable tag/note, minimal LinkGroup facts, extracted text, and the pinned local text embedding space. It also owns durable whole-subject tag and plain-text note CRUD plus portable export/import independently of index storage. It does not make any index recovery authority. LinkGroup indexing begins only after Phase 6 adds the reviewed group subject-kind authorization and feed. CLIP, additional vector or multimodal ranking, external enrichment, richer Collections, nested groups, roles, ratings, relationship graphs, typed segment annotations, and machine-suggestion review remain later or external capabilities.

Baseline requirements:

- Every indexed document binds a durable subject, index generation, and its authoritative source: snapshot plus namespace entry for file facts, or the current LinkGroup subject and membership mapping for group facts. A group mapping resolves each group-relative path to a stable file `SubjectRef`; it is not a second file identity or a version history. Processor-artifact bindings remain present where a processor produced the indexed material.
- Search resolves each hit through the current host-owned authorization and namespace layer before returning content or sensitive metadata.
- Index coverage, failed extraction, stale generations, and rebuild state remain visible.
- Deleting or rebuilding the index cannot change content identity, namespace, plans, representations, publications, or verification records.
- Processor or schema upgrades create a new derived generation; a complete validated generation is atomically selected rather than mixed in place.

The MVP annotation profile and later richer semantic profiles preserve these lifecycle distinctions:

- User tags, notes, ratings, relationships, annotations, accepted corrections, claim resolutions, and policies are authoritative user data.
- Embeddings, OCR, ASR, captions, thumbnails, and ANN indexes are disposable derivatives.
- Source metadata, sidecar metadata, external metadata, model-generated claims, and user annotations retain distinct provenance classes and revision histories.
- Annotation scope uses the canonical extensible `SubjectRef` union across asset, file version, namespace entry, entity, collection, component, virtual member, representation, and semantic segment; extensions require a versioned subject schema.
- ACL changes propagate with a recorded index watermark.
- Query-time authorization is applied after candidate retrieval so a stale projection cannot disclose newly restricted subjects.
- Stale search results are visibly marked.
- Model, tokenizer, preprocessing, prompt, parser, embedding-schema, or index-schema upgrades create new processor and index generations.
- A new generation is built completely, compared against regression and drift criteria, and atomically published; generations are never mixed in place.
- Cache TTL, quota, and eviction are explicit.
- Privacy purge and right-to-erasure propagate to derivatives unless legal hold applies.
- External enrichment retains provider, destination, query, outgoing-field, evidence-digest, license or terms, retrieval-time, confidence, TTL, cost, and egress receipts.
- Metadata writeback is a separate mutation job with protected preimage, diff, conflict check, approval, and new content version.
- MVP tag and note revisions are preserved through authenticated portable annotation bundles independently of SQLite, indexes, and deferred control-plane recovery machinery. A later profile may additionally include them in a `ControlPlaneRecoverySet`.
- Authoritative and disposable data are physically and logically distinguishable.

## 26. Accessibility and diagnostics

Every later profile that ships a supported WebUI must conform to WCAG 2.2 Level AA for its browser and assistive-technology matrix. RW-MVP-1 has no WebUI release gate.

Requirements:

- All inventory, policy, approval, key-recovery, anomaly, restore, lifecycle, and emergency-information workflows are operable by keyboard alone with visible focus, logical focus order, skip navigation, and no drag-only interaction.
- Controls expose programmatic names, roles, states, relationships, validation errors, progress, blocked reasons, and confirmation consequences to screen readers. Live updates are rate-limited and do not steal focus.
- Status, severity, fidelity, health, and graph meaning never depend on color, position, motion, sound, or an icon alone. Text equivalents and sufficient contrast are mandatory.
- Layout reflows under zoom and narrow viewports without hiding approvals, evidence, or destructive-action warnings. Text spacing and user styles do not break critical workflows.
- Reduced-motion preferences are honored. Flashing, forced animation, inaccessible time limits, and automatic dismissal of critical evidence are prohibited.
- Authentication, session expiry, approval signing, and destructive confirmations allow sufficient time, preserve entered state where safe, and provide accessible error recovery.
- If a visual policy graph is shipped, it has a complete non-visual structured editor and ordered textual representation exposing every node, edge, conflict, and validation result.
- Audio, video, image, and side-by-side fidelity previews have accessible labels and available transcripts, captions, descriptions, or equivalent evidence views; no approval requires sensory comparison as its only path.
- Every WebUI action retains an equivalent documented CLI or API operation.

Release qualification includes automated accessibility scanning plus manual keyboard, zoom/reflow, high-contrast, reduced-motion, and screen-reader tests for the named support matrix. Known accessibility defects receive severity, owner, remediation deadline, and capability-impact assessment; a critical workflow defect blocks that WebUI capability from `SUPPORTED` status.

Diagnostic bundles:

- Are exportable.
- Are redacted by default.
- Exclude raw content, secrets, private paths, tracker passkeys, peer IPs, and embeddings unless explicitly selected.
- Include trace IDs, component versions, state machines, and evidence needed for support.

Recovery instructions and evidence reports must have printable offline forms.

## 27. Cold media boundary

The MVP does not claim tape, optical, or offline-removable-media management.

When added, the model must include volume identity, set membership, site and shelf, chain of custody, required drive and reader, multi-volume ordering, media-error trends, refresh deadlines, and operator workflows.

## 28. Operational acceptance tests

1. Inject a crash at every snapshot commit boundary.
2. Resume an expired worker and confirm fencing blocks commit.
3. Remove an external drive during scan and confirm no false deletion tombstone.
4. Revoke omission approval during grace.
5. Run GC concurrently with restore, upload, repair, and migration.
6. For `RW-MVP-1`, lose the operational catalog, baseline search index, processor registry, and source host, then restore from the one qualified repository using an independently retained recovery reference, scoped credential source, independent trust anchor, compatible reader, and valid portable publication commit. Run the legacy bootstrap-corpus loss test only for a later profile that activates that corpus.
7. Restore every retained generation before, during, and after key rotation.
8. Restore native filesystem semantics and produce explicit cross-platform degradation.
9. Migrate a backend completely and recover from an old immutable manifest.
10. Remove a plugin, runtime, registry, and original CPU environment, then decode a specialized representation; retire the old decoder only after the per-instance successor bootstrap set reaches `COLD_VERIFIED`.
11. Rebuild an empty catalog and durable control-plane state from the ControlPlaneRecoverySet plus signed manifests and backend evidence.
12. Use an independent implementation to read historical manifest generations.
13. For a retained immutable capture, mutate the live source after the capture barrier and prove that inventory, repository receipt, and RRF root remain bound to the retained capture digest. For a validated-live capture, mutate files during reads and prove stable admitted bytes, detected drift, and the weaker collection-consistency claim are recorded correctly.
14. Fail an optional snapshot driver and confirm only its stronger consistency profile blocks; the generic capture profile remains available and cannot publish `CRASH_CONSISTENT` without snapshot evidence.
15. Replace, adopt, decommission, clone, and remount sources; drop watcher events; omit recursive directory coverage; use an unsupported NFS, SMB, or FUSE watcher; overflow, reset, truncate, roll back, or restart the change journal; and confirm no history continuity or tombstone is inferred without the required signed transition and full baseline.
16. Inject mass deletion, rename, and rewrite patterns and confirm new bytes remain preserved while omission, retention reduction, placement retirement, and GC are held.
17. For `RW-MVP-1`, prove clean recovery of the one exact repository without the operational catalog, baseline index, optional processors, or plaintext secret exposure. Run threshold-share loss, custodian replacement, and quorum refresh tests only for a later custody profile.
18. Stop the source host, control service, telemetry exporter, and notification worker independently and verify the external dead-man and SLO reason codes detect each failure within the declared objective.
19. For each later named profile that ships a supported WebUI, complete every profile-applicable critical workflow using keyboard-only and the declared screen-reader matrix, including non-visual policy editing and destructive confirmation paths where those capabilities exist.
20. Change an authoritative placement through degradation, repair, supersession, quarantine, or retirement and verify that a successor Envelope, Witness, Seed, replica set, and post-publication bootstrap drill return the checkpoint to `COLD_VERIFIED` before destructive lifecycle effects resume.
21. Preserve witness policies, key history, highest-seen state, fork resolutions, raw fork evidence, and quarantined branches while allowing a superseded payload placement with only evidence edges to retire.
22. Drop watcher events, reset a journal, rename a large directory, copy identical content across volumes, and change unreliable timestamps; verify that no tombstone or identity merge occurs without the required complete evidence.
23. Replace an ancestor directory, root, bind mount, mounted filesystem, snapshot exposure, or validated-live basis during traversal; race a regular path with a FIFO or device; omit boundary validation; and confirm the capture cannot become `READY` or publish namespace records.
24. For a ZFS profile, attempt snapshot destruction before and during consumers and prove the qualified hold prevents it until fenced release. For a Btrfs profile, remove read-only state, substitute or delete the subvolume, or change UUID or generation evidence and prove revalidation blocks every later consumer and publication.
25. Exercise catalog hide, restore suppression, source tombstone, stop protection, snapshot retirement, physical GC, and privacy purge independently and prove that no action inherits the side effects of another.
26. Change a default parser, extractor, or baseline index schema and prove that a new generation builds and publishes without changing recovery truth or mixing incompatible documents. Repeat with a model, tokenizer, prompt, or vector schema only for a later semantic profile.
27. Tighten source or subject authorization after index build and prove that query-time resolution blocks stale rows before they disclose metadata or content.
28. Revoke an AutomationGrant during a running job and prove that already staged preservation remains safe while the next external or risk-increasing commit fails closed.
29. Run metadata writeback and prove that the old original remains protected, the exact diff and approval are retained, and the edited object receives a new content version.
