# Database and Virtual Machine Capture Requirements

> **Profile status:** Database-aware and virtual-machine-aware capture is a staged profile above the NAS-first managed-archive MVP. `RW-MVP-1` may identify database files, virtual disks, and related configuration, expose safe type and risk evidence through baseline search, and preserve readable bytes through the generic exact route or exact fallback in one qualified mature repository. It does not claim database-native transactional consistency, guest quiescence, PITR, snapshot-chain completeness, bootability, or application health.

## 1. Purpose

RestoreWeave must treat a database or virtual machine as a versioned stateful collection whose recovery depends on coordinated state, ordered journals, configuration, keys, external dependencies, and executable validation. Copying the visible files is not sufficient evidence of application-consistent recovery.

This document defines later-phase requirements for:

- Database discovery, physical base backups, write-ahead or transaction-log continuity, point-in-time recovery, global objects, extensions, external objects, restoration, and validation.
- Virtual-machine discovery, disk and snapshot-chain capture, guest quiescing, memory and device state, firmware identity, attached storage, hypervisor portability, isolated boot, and application-health validation.
- Separation of read-only collection resolution, privileged capture coordination, restoration, and independent validation.
- Durable manifest and API records, lifecycle management, security controls, qualification gates, and release acceptance tests.

This document is governed by:

- [Product Requirements](product-requirements.md) for product scope and phase gates.
- [MVP and Operator Contract](mvp-and-operator-contract.md) for the NAS-first managed-archive `RW-MVP-1` boundary.
- [Protection Policy and Planning Requirements](protection-policy-and-planning.md) for RPO, RTO, replica, retention, drill, privacy, and dependency objectives.
- [Extension System Requirements](plugin-system.md) for independently permissioned plugin entry points.
- [Restore Manifest Requirements](restore-manifest.md) for authenticated publication and portable recovery records.
- [Operations and Lifecycle Requirements](operations-and-lifecycle.md) for capture consistency, retention, garbage collection, migration, and restore drills.
- [Recovery Fidelity Requirements](recovery-fidelity.md) for component outcomes and validation evidence.
- [Security and Threat Model](security-and-threat-model.md) for privileged host operations, hostile inputs, secrets, and isolated execution.

## 2. Scope and non-goals

Database and virtual-machine support does not:

- Replace engine-native backup, log-archive, hypervisor, volume-snapshot, or guest-agent mechanisms.
- Infer transactional consistency from file hashes, successful upload, a storage snapshot, a hypervisor snapshot, or a clean filesystem mount.
- Treat a logical database export as equivalent to a physical base backup unless a component contract explicitly accepts the logical outcome.
- Treat a VM snapshot retained on the source hypervisor as an independent backup.
- Promise cross-hypervisor resume, device-bound-key portability, or application health merely because a disk image converts or boots.
- Execute restored workloads on a production network or attach production storage by default.
- Contact external databases, object stores, key services, hypervisors, control planes, or guest agents without a scoped capability and network policy.
- Make database-aware or VM-aware capture part of `RW-MVP-1`.

Under `RW-MVP-1`, database files, virtual disks, configuration files, and related paths use the ordinary generic exact filesystem lane. The selected `CaptureDriver` records its actual consistency class, including a best-effort live view or a stronger snapshot-backed view when independently qualified. The baseline catalog may report format, size, path, processing state, and unresolved stateful-recovery risk, but it makes no database-native, guest-quiesced, PITR, snapshot-chain, bootability, or application-health claim. Every stateful-capture requirement in this document is deferred from the MVP release gate, and processor or resolver failure leaves exact fallback available.

## 3. Governing invariants

1. Collection resolution produces evidence and dependency membership; it cannot freeze a workload, take a snapshot, omit bytes, restore, execute, or approve a recovery outcome.
2. A stateful `CaptureDriver` profile coordinates engine, hypervisor, guest, and storage mechanisms; it cannot independently certify its own result.
3. A `Processor.VALIDATE` capability must consume immutable captured inputs and evidence. It receives no authority to modify the source workload or publish a stronger contract.
4. A stronger consistency level is achieved only when its declared barrier, component closure, chain continuity, and independent validators pass.
5. Unknown, unsupported, encrypted, changing, incomplete, conflicting, or partially captured state defaults to exact preservation with an explicit unresolved status.
6. Database logs and VM delta chains are ordered dependencies, not interchangeable blobs. A missing, duplicated, divergent, or unverifiable interval blocks every recovery target that crosses it.
7. A successful engine restore or guest boot does not prove that required roles, extensions, external objects, applications, or business invariants are healthy.
8. A point-in-time or machine-state target is immutable and engine- or hypervisor-specific. It must never be inferred from wall-clock time alone when an authoritative log or state position exists.
9. Capture failure must not leave a database in backup mode, a guest filesystem frozen, a VM paused, or source retention advanced without a bounded and audited recovery action.
10. Ordinary manifests and API responses contain secret references and redacted identifiers, never plaintext database credentials, encryption keys, vTPM secrets, memory contents, or hypervisor tokens.
11. Recovery validation is isolated from production identities, networks, storage, schedulers, replication peers, and external side effects unless each dependency is deliberately simulated or separately authorized.
12. Source-native backup and snapshot identifiers are evidence, not permanent RestoreWeave content identities.

## 4. Common stateful-workload model

### 4.1 Stateful collection identity

A stateful collection extends the common entity model with:

- `stateful_collection_id`: stable identity of one database cluster or instance, virtual machine, or explicitly defined consistency group.
- `stateful_collection_revision_id`: immutable snapshot-scoped membership, topology, configuration, and dependency resolution.
- `capture_profile_id` and digest: the exact qualified database-engine or hypervisor profile.
- `source_system_identity`: engine system ID, database incarnation, hypervisor VM identity, or another profile-defined stable identifier.
- `consistency_group_id`: an optional identity for components that must share one barrier.
- `component_revision_ids`: immutable component memberships and roles.
- `external_dependency_ids`: referenced services, storage objects, keys, devices, and operator actions outside the captured byte set.

A path, process name, socket, port, database name, VM display name, hypervisor inventory path, or cloud instance label is evidence and not sufficient identity by itself.

### 4.2 Required common component roles

| Role | Examples | Default treatment |
| --- | --- | --- |
| `PRIMARY_STATE` | Database data files or VM disks. | Exact authoritative capture. |
| `ORDERED_JOURNAL` | WAL, transaction logs, archived redo, binlog, or another replay stream. | Exact, ordered, continuity-validated. |
| `RUNTIME_CONFIGURATION` | Engine settings, VM hardware configuration, boot policy, and service definitions. | Exact plus parsed semantic inventory. |
| `IDENTITY_AND_GLOBAL_STATE` | Database system catalogs and globals, VM UUIDs, firmware variables, and machine identity. | Exact, sensitivity-scoped. |
| `EXTENSION_OR_DEVICE_DEPENDENCY` | Database extensions, plugins, virtual devices, drivers, and guest tools. | Exact or immutable source-bound dependency with a validated fallback. |
| `SECRET_OR_KEY_DEPENDENCY` | TDE keys, KMS references, vTPM state, device keys, and credentials. | Protected secret reference or dedicated encrypted recovery object. |
| `EXTERNAL_STATE` | Tablespaces, object stores, foreign servers, attached disks, shared volumes, and passthrough devices. | Captured in the same barrier or explicitly unresolved. |
| `VALIDATION_PROFILE` | Engine checks, boot probes, queries, and application-health tests. | Immutable qualified dependency. |
| `RECOVERY_TOOLCHAIN` | Engine binaries, hypervisor readers, converters, drivers, schemas, and restore scripts. | Retained or reproducibly source-bound dependency. |

### 4.3 Consistency levels

The common capture levels remain:

- `CONSISTENCY_UNVERIFIED`
- `CRASH_CONSISTENT`
- `APPLICATION_QUIESCED`
- `APPLICATION_EXPORTED`
- `TRANSACTIONALLY_CONSISTENT`
- `OFFLINE_CONSISTENT`

These values describe capture conditions, not a universal quality ranking. A policy names the exact acceptable levels for each component and profile. For example, an engine-native logical export may be transactionally consistent for selected schemas while omitting physical roles, extensions, or replication state required by another contract.

The achieved level is recorded only after independent validation. A failed stronger attempt may publish a weaker result only when that exact fallback is predeclared, all required evidence for the weaker result exists, and no source-side safety issue remains.

### 4.4 Recovery targets and barriers

Every stateful capture binds:

- One immutable source observation and collection revision.
- A start and end time from trusted monotonic and wall-clock sources.
- A profile-defined consistency barrier.
- Every component expected to participate in the barrier.
- Per-component start and completion positions.
- Maximum measured cross-component skew.
- A declared recovery target or target range.
- The exact validator profile and required evidence.

Wall-clock timestamps are explanatory evidence. Engine-native log positions, snapshot generations, delta-parent identities, and captured barrier tokens are authoritative where the profile provides them.

## 5. Approved seam mapping and capability separation

Database and VM support uses the existing `Processor` and `CaptureDriver` seams plus core-owned restore execution. A package may implement more than one role only through independently negotiated capabilities with distinct configuration, lifecycle state, jobs, audit records, and conformance tests.

### 5.1 `Processor.PARSE` and `Processor.ENRICH` collection resolution

A database or VM collection-resolution processor profile:

- Reads only declared local metadata, configuration, inventory APIs, and bounded engine or hypervisor metadata endpoints.
- Identifies candidate collections, components, identities, topology, dependency edges, profile applicability, and known blind spots.
- Uses read-only credentials when an API is required.
- Has no freeze, pause, snapshot, backup, retention, restore, process-control, external-acquisition, execution, or omission authority.
- Returns proposed identities, membership, dependencies, coverage, provenance, conflicts, and a claimed `RESOLVED_COMPLETE`, `RESOLVED_PARTIAL`, `CONFLICTING_MEMBERSHIP`, `STALE_PROFILE`, `UNSUPPORTED_VERSION`, `PERMISSION_BLOCKED`, `SOURCE_INCOMPLETE`, or `UNKNOWN` state with field-level evidence. The host validates and publishes the authoritative collection-resolution record; user corrections and acceptance decisions are referenced by, never authored by, the processor.

### 5.2 Stateful `CaptureDriver` profile

A database-aware or VM-aware `CaptureDriver` profile:

- Executes one signed, versioned capture plan for one collection revision and profile.
- Receives only the engine, hypervisor, guest-agent, volume-snapshot, process-control, filesystem, and secret-reference capabilities required by that plan.
- Establishes and records consistency barriers, starts and stops native backup modes, coordinates quiesce and thaw, and copies or exports declared components.
- Has bounded deadlines, fencing, retry semantics, cancellation checkpoints, and an emergency source-resume path.
- Cannot change the recovery contract, omit an unresolved component, advance destructive source retention, certify validation, or publish a committed snapshot by itself.

### 5.3 Core-owned restore execution

The core-owned database or VM restore executor:

- Resolves the complete immutable dependency closure.
- Creates an isolated recovery destination.
- Reconstructs engine files, ordered logs, VM disks, configuration, firmware, and declared external dependencies in profile-defined order.
- Applies conversion or replay only through a pinned toolchain and immutable plan.
- Does not connect recovered workloads to production networks, identities, replication peers, key services, or storage without separate authorization.
- Cannot declare recovery success; it emits staged outputs and restore evidence for independent `Processor.VALIDATE` capabilities.

### 5.4 `Processor.VALIDATE`

Database and VM `Processor.VALIDATE` capability profiles are independent of collection resolution, capture, and core-owned restore execution. They:

- Receive read-only access to immutable captured or restored subjects.
- Declare whether a check is structural, cryptographic, engine-native, logical, boot, application-functional, or external-dependency validation.
- Record complete inputs, toolchain digests, environment, coverage, thresholds, results, warnings, and side effects.
- Cannot mutate the source, accept a weaker outcome, publish a manifest, contact production, or approve omission.
- Must distinguish `PASS`, `FAIL`, `PARTIAL`, `BLOCKED`, and `NOT_APPLICABLE`; absence of a check is never a pass.

### 5.5 Separation enforcement

The host must reject:

- A collection-resolution processor requesting snapshot, backup-mode, pause, guest-freeze, or source-retention capabilities.
- A `CaptureDriver` requesting publication-signing or validation-approval authority.
- A `Processor.VALIDATE` capability reusing source-write, hypervisor-admin, or production-network credentials.
- A single opaque script that discovers, captures, deletes source journals, restores, and marks success without independently committed boundaries.
- A package-level permission grant inherited by entry points that do not declare and justify it.

## 6. Common capture lifecycle

Every stateful capture follows a fenced, resumable lifecycle:

~~~text
resolve collection
-> compile immutable capture plan
-> preflight source and destination
-> arm emergency resume
-> enter native backup or quiesce mode
-> establish consistency barrier
-> capture every required component
-> capture closing positions and dependency evidence
-> leave backup mode or thaw source
-> verify source resumed safely
-> independently validate staged capture
-> publish through the ordinary signed snapshot protocol
~~~

Required behavior:

- The plan binds source identity, collection revision, profile digest, protection objective, expected components, barrier method, timeouts, resource budgets, credentials, destinations, and validators.
- Preflight verifies version support, privileges, capacity, temporary space, source health, current backup or snapshot state, required log or delta retention, and emergency-resume feasibility.
- The emergency-resume action is prepared before the first source mutation and remains usable if the worker crashes or loses control-plane contact.
- Every source-side operation is idempotent or has a profile-defined reconciliation procedure.
- Capture outputs remain staged and non-authoritative until validation and normal snapshot publication complete.
- A publication records the achieved consistency outcome and all excluded, missing, degraded, or externally dependent components.
- Source cleanup or retention advancement is a later fenced action and cannot be part of the `CaptureDriver` success response.

## 7. Database collection resolution

A qualified database profile must resolve, where applicable:

- Engine family, edition, exact version, patch level, architecture, and server or cluster identity.
- Database incarnation, timeline, recovery fork, system ID, cluster UUID, and current engine-native log position.
- Instances, clusters, databases, schemas, catalogs, tablespaces, filegroups, data directories, control files, undo state, and engine metadata.
- Physical paths, mount or volume identities, storage classes, sparse or direct-I/O behavior, and required filesystem semantics.
- Replicas, primary or standby role, replication slots, publications, subscriptions, CDC offsets, log-shipping state, and failover topology.
- Configuration files, service definitions, environment references, locale, collation, encoding, timezone data, and engine parameters that affect interpretation.
- Roles, users, grants, ownership, row-level policies, password verifiers, certificates, scheduled jobs, event definitions, and global objects.
- Extensions, plugins, modules, user-defined functions, procedural runtimes, shared libraries, and their exact versions and configuration.
- Encryption mode, TDE or tablespace encryption, key-provider identity, key-version references, certificates, and recovery-key dependencies.
- External tables, foreign data wrappers, linked servers, database links, object-store references, filesystem large objects, external LOBs, search indexes, queues, and other state not contained in the primary files.
- Engine-native backup manifests, checksums, page-checksum settings, corruption status, and known unsupported features.

The resolver must state which declared roots and metadata surfaces it inspected. A profile cannot report complete resolution if any required database, tablespace, global object, log archive, extension root, or external dependency was inaccessible.

## 8. Database base-backup requirements

### 8.1 Capture forms

A database capture profile may support:

- Engine-native physical base backup.
- Storage-level snapshot coordinated with an engine-native barrier.
- Offline physical capture after verified clean shutdown.
- Engine-native logical export as an independently typed representation.
- Supplemental schema, role, configuration, or metadata exports.

A logical export never silently substitutes for a physical base backup. Each form declares the components, physical state, transactional state, global objects, privileges, extensions, large objects, and engine-specific features it preserves or omits.

### 8.2 Base-backup boundary

Every physical base backup records:

- Source system identity, engine version, backup method, toolchain, and profile.
- Backup start, barrier, checkpoint, and completion times.
- Engine-native start and end positions.
- The earliest log position required to make the backup recoverable.
- The latest recovery target proven contiguous at publication time.
- Timeline, incarnation, reset-log, or recovery-fork identity.
- Required control, manifest, label, history, and checksum files.
- Included databases, tablespaces, filegroups, volumes, and excluded temporary or rebuildable components.
- Per-file or per-object content identities and engine-native checksums where available.
- Whether reads occurred from a primary, replica, snapshot, or exported stream and the lag or replay position at the barrier.

Profiles must define the precise inclusivity of start and end positions. Ambiguous boundaries block PITR qualification.

### 8.3 Physical capture safety

- Copying live database files without a qualified native barrier or atomic storage snapshot cannot claim more than `CRASH_CONSISTENT`.
- Engine-native backup start and stop calls must be paired and reconciled after failure.
- Every volume containing required data must participate in one supported consistency mechanism or be reported outside the barrier.
- A replica-sourced backup records replay delay, paused or active replay state, primary lineage, and whether all required logs are obtainable.
- The profile must specify treatment of unlogged, memory-only, temporary, delayed-write, or externally persisted state.
- A successful file copy with a failed closing checkpoint, backup label, or log-position read is incomplete.

## 9. WAL, redo, transaction-log, and PITR requirements

### 9.1 Tagged log positions

RestoreWeave stores engine positions as tagged values, never as an untyped integer or timestamp. Supported profile variants may include:

- PostgreSQL LSN plus timeline and history identity.
- MySQL or MariaDB GTID set, binary-log file and offset, and required redo metadata.
- SQL Server LSN, backup-set identity, recovery fork, and log-chain metadata.
- Oracle SCN, incarnation, redo thread, sequence, and archived-log identity.
- A profile-defined opaque position with a canonical comparator and validator.

Each profile defines canonical serialization, comparison, ordering, inclusivity, rollover behavior, fork detection, and conversion to a human-readable form. Positions from different systems, timelines, incarnations, or forks are incomparable.

### 9.2 Continuous journal capture

An ordered-journal record contains:

- Source system, timeline or incarnation, stream or thread, segment identity, and immutable content identity.
- Native start and end positions with explicit inclusive or exclusive semantics.
- Predecessor and successor evidence where the engine provides it.
- Native checksum, RestoreWeave hash, length, archive time, and source location.
- Compression or encryption representation, key reference, and decode dependencies.
- Acquisition method, archive command or agent identity, retries, duplicate observations, and final durable placements.
- Whether the segment is complete, partial, still open, synthesized, repaired, or superseded.

Open or partial segments cannot close a continuity interval unless the engine profile provides and validates an exact safe-prefix rule.

### 9.3 Log-gap detection

The system must derive continuity by engine-native positions and lineage, not filenames or timestamps alone.

A gap exists when:

- A required interval has no authenticated segment coverage.
- Segment boundaries overlap inconsistently or leave an uncovered position.
- Two non-identical segments claim the same identity or interval.
- A timeline, incarnation, reset-log, recovery-fork, or primary lineage changes without the required signed history evidence.
- An archived segment is truncated, corrupt, undecodable, or encrypted under an unavailable key.
- A multi-threaded redo profile lacks a required thread interval.
- The engine cannot replay a supposedly continuous sequence during an isolated drill.

Every gap records the last proven recoverable position, first unproven position, affected targets, detection time, source-retention risk, and remediation state. A later segment does not heal an earlier gap unless the exact missing interval is recovered and the complete chain is revalidated.

### 9.4 PITR target semantics

A recovery target may be:

- An exact engine-native log position.
- A transaction or commit identifier supported by the profile.
- A named restore point with immutable creation evidence.
- A wall-clock time resolved by the engine against a continuous authenticated log range.
- The latest contiguous position proven during the restore attempt.

The requested target, resolved engine position, stop condition, achieved position, and stop evidence are all retained. A restore that passes beyond, stops before, or cannot prove the requested target is not a successful PITR result.

RPO health is derived from the latest durable and independently readable continuous position, not from the newest uploaded filename, source checkpoint, or archive-agent heartbeat.

### 9.5 Source retention

RestoreWeave must not request source log deletion, slot advancement, archive pruning, or backup expiration until:

- A qualifying base backup and every required journal interval have the objective's required independent durable placements.
- Hash, decode, ordering, and chain validation pass.
- At least one isolated replay proves the retained boundary under the applicable drill policy.
- No legal hold, active restore, migration, repair, or grace period retains the source copy.
- A separately authorized retention job commits the exact deletion range under fencing.

## 10. Database roles, extensions, and external objects

### 10.1 Global and security objects

Profiles must inventory and independently protect engine-global state that ordinary per-database backups may omit, including roles, ownership, grants, authentication configuration, password verifiers, certificates, resource groups, scheduled jobs, and server-level settings.

Secret or authentication material is encrypted under an appropriate protection scope and referenced from ordinary manifests. Reports expose presence, key identity, version, and recovery readiness without exposing values.

Restoration of principals must support explicit identity mapping and conflict policy. It must not overwrite production identities or reuse production passwords during an isolated drill.

### 10.2 Extensions and executable dependencies

For every extension, plugin, procedural language, user-defined native module, or shared library, the collection records:

- Logical extension identity and database catalog version.
- Exact package, binary, control, schema, and configuration identities.
- Operating-system, architecture, runtime, ABI, and engine-version constraints.
- Installation and upgrade order.
- Required privileges and unsafe code capabilities.
- Immutable source binding or exact retained bytes.
- Static and isolated dynamic validation requirements.

A schema declaration without compatible binaries is incomplete. Extension installation and migration code is hostile executable content and runs only in an isolated recovery environment with explicit authority.

### 10.3 External objects and services

External tables, linked or foreign servers, object-store objects, filesystem LOBs, search services, queues, secrets managers, and other external state must be represented as explicit components or dependencies.

For each dependency, the manifest states:

- Stable provider and object identity, version, region, namespace, and expected digest or semantic invariant where available.
- Whether bytes are included, source-bound, separately protected, simulated, or unavailable.
- Required credentials, network, rights, residency, and key references.
- Consistency relationship and maximum permitted skew from the database barrier.
- Restore order and validation procedure.
- Approved degraded behavior when the dependency cannot be recovered.

An external locator alone does not prove durability or equality. Validation processors do not contact a live external service by default; they use a captured fixture, isolated test endpoint, or separately approved read-only request.

## 11. Database restoration and validation

### 11.1 Restore environment

Database restoration occurs in a clean, isolated environment with:

- Pinned engine binaries, operating system or container image, extensions, locale, timezone data, and configuration dependencies.
- No production listener, replication peer, scheduler, webhook, email, external writer, or unrestricted network access.
- Separate test credentials and collision-safe identities.
- Read-only source representations and a staged writable destination.
- Resource limits sufficient to distinguish capacity failure from data corruption.

### 11.2 Required restore sequence

The core-owned restore executor must:

1. Verify all base-backup, journal, configuration, extension, key, and external-dependency identities.
2. Reconstruct physical layout and engine-required filesystem semantics.
3. Install or bind the exact qualified engine and extension toolchain.
4. Configure recovery without production endpoints or credentials.
5. Replay the authenticated continuous log chain to the exact requested target.
6. Record the achieved stop position and recovery-fork identity.
7. Open the database only under the profile's isolated validation mode.
8. Run structural, physical, logical, security-object, extension, and application-specific `Processor.VALIDATE` profiles.
9. Publish per-component outcomes and retain the complete restore evidence.

### 11.3 Validation layers

At minimum, a qualified database profile defines:

- Cryptographic verification of every retained input and decoded representation.
- Engine-native backup-manifest, page-checksum, control-file, catalog, and recovery-log checks where supported.
- Proof that replay reached and stopped at the requested native position.
- Successful engine startup and clean recovery completion without ignored fatal errors.
- Database, schema, table, index, large-object, sequence, role, grant, extension, and collation inventories against captured expectations.
- Profile-defined logical invariants, representative read-only queries, row or object counts, and sampled or complete content checks.
- External-object availability or explicit simulated and blocked results.
- Application-level health checks where the protection contract requires them.

Engine startup alone is insufficient. A check skipped because of cost, permissions, unavailable keys, unsupported versions, or a missing external dependency is `PARTIAL` or `BLOCKED`, not `PASS`.

## 12. Virtual-machine collection resolution

A qualified VM profile must resolve:

- Hypervisor, management plane, exact versions, host architecture, cluster, datastore, and VM generation.
- Stable VM identity, instance UUID, firmware UUID, display name, inventory path, guest type, and clone lineage.
- Power, pause, suspend, snapshot, migration, and guest-agent state at observation time.
- Virtual CPU model, topology, feature mask, NUMA policy, memory size, huge-page policy, and hardware-virtualization dependencies.
- Chipset, firmware, boot order, secure-boot state, UEFI variables, NVRAM, vTPM, virtual device model, and machine-type version.
- Every virtual disk, backing object, parent or overlay, controller, bus, unit, cache mode, provisioning mode, virtual size, and datastore identity.
- Every hypervisor snapshot, disk snapshot, memory snapshot, delta parent, and snapshot-tree branch.
- Network adapters, MAC addresses, virtual switches, VLANs, addresses, cloud-init or guest customization, and production collision risks.
- Attached volumes, shared disks, raw-device mappings, host paths, ISO or floppy media, USB or PCI passthrough, GPUs, dongles, and external storage.
- Guest-agent version and capabilities, guest filesystem and volume topology, encryption state, application quiesce providers, and known unsupported workloads.
- Hypervisor-side encryption, KMS or key-provider references, and device-bound or host-bound recovery dependencies.

Collection completeness is profile-relative and fails closed when the resolver cannot enumerate a declared datastore, backing object, snapshot parent, attached volume, firmware object, or key dependency.

## 13. VM disk, configuration, and snapshot-chain capture

### 13.1 Disk component record

Each virtual disk records:

- Stable logical disk role and source identity.
- Exact format, virtual size, allocated extents, sparse semantics, block size, and content identities.
- Controller, bus, unit, boot role, caching and discard behavior, and guest-visible identity.
- Encryption and key reference.
- Backing object, parent, overlay, or delta dependency.
- Hypervisor snapshot and change-tracking generation at the barrier.
- Capture method, full or incremental type, immutable output identity, and required reader or converter.

The VM configuration and disk set are one collection revision. A disk image without its required controller, backing parent, encryption key, or firmware context is not a complete machine capture.

### 13.2 Snapshot and delta chains

For each VM disk chain, RestoreWeave must retain:

- Ordered node identities from a self-contained base through the selected recovery node.
- Exact parent digest and source-native parent identity for every delta.
- Snapshot-tree branch identity and barrier time.
- Change-block-tracking epoch, generation, token, bitmap digest, and invalidation events.
- Complete block-range coverage and overlap rules.
- The toolchain required to read, merge, flatten, or convert every node.

A chain is invalid when a parent is absent, a delta points to a different parent, a tracking epoch resets without a new base, block coverage is ambiguous, or source and captured ancestry diverge. A source hypervisor's claim that a snapshot exists is not a substitute for independent readback and content verification.

Incremental or changed-block capture begins only from a qualified full baseline. Periodic self-contained rebases limit chain length and reader risk. Flattening or consolidation creates a new representation and never rewrites historical chain identity; the old closure remains protected until the new representation passes independent restore validation and retirement policy.

### 13.3 Configuration capture

The capture includes an immutable, normalized machine-configuration record plus exact source-native configuration artifacts where available. It covers CPU and memory topology, machine type, firmware, controllers, disks, networks, clocks, boot order, device models, serial and console settings, guest tools, resource policies, and all profile-declared settings that affect boot or interpretation.

Defaults supplied by a future hypervisor version must not silently replace an omitted source value. Unknown or unsupported configuration is retained exactly and reported as a portability blocker.

## 14. Guest quiesce, memory, and running state

### 14.1 Guest quiesce

Guest-quiesced capture follows a bounded workflow:

~~~text
verify guest-agent identity and capability
-> run approved application pre-freeze hooks
-> freeze declared guest filesystems
-> establish the cross-disk snapshot barrier
-> create immutable disk capture points
-> thaw every frozen filesystem
-> run approved post-thaw checks
-> verify guest and applications resumed
~~~

Requirements:

- The plan enumerates every guest filesystem and application expected to quiesce.
- Hooks are signed, allowlisted, structured, timeout-bounded, and have no ambient network or shell authority.
- Partial freeze, missing volume participation, hook failure, agent disconnect, timeout, or uncertain thaw blocks `APPLICATION_QUIESCED`.
- Emergency thaw is attempted after success, failure, cancellation, host loss, and worker fencing. An unresolved freeze state is a critical source-safety incident.
- A guest-filesystem freeze does not establish transactional consistency for an active database unless a qualified database-aware hook and independent `Processor.VALIDATE` evidence prove it.
- Cross-disk skew and snapshot completion order are measured and compared with profile limits.

### 14.2 Offline and crash-consistent capture

An offline capture verifies guest shutdown, absence of suspended write state, disk closure, and profile-defined clean-power evidence before claiming `OFFLINE_CONSISTENT`.

A live hypervisor snapshot without successful guest quiesce may claim only the explicitly supported crash-consistent outcome. A later clean boot does not retroactively prove that the captured application state was transactionally consistent.

### 14.3 Memory and suspended execution state

Memory or suspended-state capture is optional and independently protected. A resumable machine-state representation must bind:

- Exact RAM pages and compression or sparse-page encoding.
- Virtual CPU registers, feature mask, timers, interrupt state, and device-emulator state.
- Hypervisor, machine-type, firmware, microcode, host-architecture, and device-model compatibility.
- Disk snapshot identities at the same suspend barrier.
- vTPM, encrypted-memory, passthrough-device, and key dependencies.
- Resume toolchain, tested destination profile, and validator result.

Memory contains credentials, session keys, decrypted data, tokens, and personal content. It is encrypted under a high-sensitivity policy, excluded from semantic extraction and cross-tenant deduplication by default, and never exposed through ordinary manifests, logs, previews, or search.

If exact resume cannot be validated, memory remains a forensic or best-effort component and does not improve the disk capture's bootability claim.

## 15. Firmware, NVRAM, vTPM, and device-bound identity

VM capture must independently represent:

- BIOS or UEFI firmware family and exact code dependency.
- UEFI variable store and NVRAM bytes.
- Secure Boot enablement, keys, databases, revocation lists, and enrolled-policy references.
- vTPM state, endorsement identity, storage roots, ownership state, and key-provider dependencies.
- VM encryption metadata, KMS or host key-provider identity, and recovery procedures.
- SMBIOS, UUID, serial, MAC, virtual disk serial, and other identities consumed by software or activation.
- Passthrough devices, virtual hardware security modules, dongles, GPUs, and device-specific keys.

Secrets and private key material use dedicated encrypted objects or secret-store references. Hashes of unavailable keys prove neither possession nor recoverability.

When device-bound state is non-exportable, the recovery contract records `BLOCKED_DEVICE_DEPENDENCY`, the exact affected components, supported manual recovery, and any weaker acceptable cold-boot outcome. RestoreWeave must not claim portability or bypass activation, Secure Boot, TPM sealing, DRM, or access controls.

## 16. Attached volumes and cross-component consistency

Every attached storage component is classified as:

- Included in the same atomic or coordinated barrier.
- Included with measured, policy-accepted skew.
- Independently captured with an application-level reconciliation procedure.
- Reacquirable through a qualified immutable source binding.
- Deliberately excluded under an explicit component outcome.
- Missing, unavailable, unsupported, or blocked.

This applies to virtual disks, shared disks, guest-mounted network volumes, host path mappings, raw devices, object-backed disks, and application data outside the VM inventory object.

The capture plan names every expected participant before the barrier. A newly discovered or detached volume during capture invalidates completeness until the collection is resolved again. Multi-writer shared disks require a profile that coordinates every writer; quiescing one VM is insufficient.

## 17. Hypervisor portability

Portability is recorded as explicit component outcomes rather than inferred from an image format. Profiles may validate:

- Native resume on the same qualified hypervisor and machine type.
- Native cold boot on a compatible hypervisor profile.
- Converted cold boot on a different hypervisor profile.
- Guest-filesystem accessibility without machine boot.
- Disk-content recovery only.

A portability plan inventories and validates:

- Source and destination architectures and CPU feature requirements.
- Disk formats, sector sizes, controllers, boot drivers, and conversion semantics.
- BIOS versus UEFI, NVRAM, Secure Boot, and vTPM support.
- Device models, network interfaces, graphics, passthrough devices, and guest tools.
- Sparse allocation, discard, snapshots, encryption, and key handling.
- Identity collision, activation, licensing, and clock behavior.
- Every intentional configuration change and its functional impact.

Converters are versioned `Processor.TRANSFORM` capabilities with golden images, byte-coverage evidence, deterministic configuration, and independent boot and filesystem `Processor.VALIDATE` profiles. A successful conversion command is not a passing portability result.

Unsupported devices or keys remain explicit blockers. RestoreWeave does not silently remove them, substitute a different firmware, reset a vTPM, change boot mode, or inject drivers unless the signed restore plan and component contract authorize that transformation.

## 18. Isolated boot and application-health validation

### 18.1 Isolation boundary

Dynamic VM validation boots only in a disposable validation environment that provides:

- No bridged production network, production VLAN, shared datastore write access, or production control-plane identity.
- Default-deny egress, DNS, metadata-service, multicast, discovery, and inbound access.
- Collision-safe temporary MAC, IP, hostname, machine, cluster, and application identities while preserving the captured identities as evidence.
- Simulated or explicitly approved dependencies for directory services, KMS, object stores, databases, queues, license servers, and time sources.
- Disabled schedulers, backup agents, replication, cloud sync, auto-update, webhooks, email, and destructive automation unless the test explicitly requires a safe substitute.
- Malware containment, resource quotas, console capture, and an automatic teardown deadline.

The validation environment and its network policy are immutable recorded dependencies. A VLAN label or UI checkbox alone is not proof of isolation.

### 18.2 Validation layers

A qualified VM profile distinguishes:

- Disk-chain and block-integrity validation.
- Filesystem replay, mount, and integrity validation.
- Firmware and boot-loader validation.
- Hypervisor power-on or resume validation.
- Guest kernel and service-manager readiness.
- Guest-agent readiness.
- Required volume and device availability.
- Application-specific health and read-only functional checks.
- External-dependency simulation or explicit blocked results.

Booting to a login screen is not sufficient when the contract requires database, service, game-server, application, or workflow health. Application checks must declare exact endpoints, queries, fixtures, timeouts, expected invariants, and permitted side effects.

Validation records preserve console logs, structured probe results, crash evidence, kernel or service errors, filesystem repair actions, achieved boot stage, and every deviation from the captured configuration. Automatic repair may be tested on a clone, but it creates a distinct transformed representation and cannot rewrite the original capture.

## 19. Security, secrets, privacy, and source safety

### 19.1 Privileged control

Database and hypervisor capture often requires high-impact privileges. RestoreWeave must:

- Use short-lived, subject-scoped capability tokens and secret references.
- Separate read-only discovery, backup, guest-quiesce, snapshot, export, restore, and destructive-retention identities.
- Prefer native structured APIs or signed allowlisted executables with structured arguments; shell command strings are prohibited authority.
- Fence every mutating request by attempt, source identity, expected state, and immutable plan digest.
- Record redacted request and response evidence without storing access tokens or sensitive command output.
- Revalidate package trust, source state, approvals, credential scope, trusted time, and revocation epoch before each source mutation.

### 19.2 Secret handling

Secret-bearing material includes database passwords and verifiers, replication credentials, TDE or KMS keys, database links, hypervisor credentials, VM encryption keys, vTPM state, Secure Boot private keys, cloud-init secrets, memory dumps, and application tokens.

Requirements:

- Ordinary catalog, manifest, event, log, prompt, diagnostic, and API views use redacted identities and secret-reference IDs.
- A digest does not replace a recoverable secret dependency.
- Secret export requires a field-scoped capability, protection policy, encryption recipient set, audit event, and restore test.
- Secret references record provider, key version, residency, expiry, revocation, availability, and offline-recovery behavior.
- Validation uses test credentials or simulated dependencies unless production access is separately approved and read-only.

### 19.3 Hostile restored state

Database pages, logs, extensions, VM disks, firmware variables, memory, guest tools, hooks, and configuration are untrusted input. Parsers, converters, engine binaries, hypervisors, and validation processors run under the security profile's isolation and resource limits. A restored VM or extension can be malicious even when every byte hash matches.

## 20. Manifest and durable record requirements

The signed restore manifest references immutable records rather than embedding mutable engine or hypervisor state.

### 20.1 Common records

Required common records include:

- `StatefulCollectionResolutionRecord`: source identity, profile, components, topology, evidence, completeness, conflicts, and external dependencies.
- `StatefulCapturePlan`: collection revision, objective, expected components, barrier, timeouts, capabilities, destinations, validators, and fallback rules.
- `StatefulCaptureAttemptRecord`: attempt identity, fencing token, source-side transitions, timestamps, checkpoints, errors, emergency-resume actions, and staged outputs.
- `ConsistencyBarrierRecord`: method, participants, native positions, snapshot tokens, measured skew, start and release evidence, and achieved source state.
- `StatefulRestorePlan`: requested collection revision, target, dependency closure, destination profile, conversion or replay steps, isolation, and validation profiles.
- `StatefulRestoreValidationRecord`: immutable restored subject, environment, toolchain, checks, coverage, per-component results, achieved target, side effects, and final status.

### 20.2 Database records

Required tagged database records include:

- `DatabaseSystemRecord`
- `DatabaseBaseBackupRecord`
- `DatabaseLogSegmentRecord`
- `DatabaseLogContinuityRecord`
- `DatabaseRecoveryTargetRecord`
- `DatabaseGlobalObjectInventory`
- `DatabaseExtensionInventory`
- `DatabaseExternalDependencyRecord`
- `DatabaseRestoreResultRecord`

The log-continuity record binds source system, timeline or incarnation, ordered interval coverage, gaps, conflicts, latest durable contiguous position, maximum validated recovery target, required base backups, and replay-test evidence.

### 20.3 Virtual-machine records

Required tagged VM records include:

- `VirtualMachineSystemRecord`
- `VirtualMachineConfigurationRecord`
- `VirtualMachineDiskRecord`
- `VirtualMachineSnapshotChainRecord`
- `VirtualMachineChangeTrackingRecord`
- `VirtualMachineGuestQuiesceRecord`
- `VirtualMachineMemoryStateRecord`
- `VirtualMachineFirmwareAndSecurityStateRecord`
- `VirtualMachineExternalDeviceRecord`
- `VirtualMachineBootValidationRecord`

The snapshot-chain record binds every base and delta content identity, parent relation, branch, change-tracking epoch, disk barrier, required reader, and independent readback result.

### 20.4 Publication and portability

- Manifest records bind stable RestoreWeave identities and signed placement receipts, not mutable VM inventory paths, engine backup names, or source repository paths.
- Large database logs, disks, memory, and backup payloads remain external content-addressed representations and do not traverse the control API.
- Every record has a schema ID, canonical digest, profile digest, producer identity, creation time, sensitivity label, and complete dependency references.
- The portable recovery closure includes the minimum engine, log reader, hypervisor disk reader, converter, firmware, schema, extension, key instructions, validation profiles, and conformance vectors needed by the contract.
- Missing secret or hardware dependencies are represented as explicit blocked components and never omitted from the manifest because their bytes cannot be exported.

## 21. REST API requirements

Stateful capture APIs are phase-gated and use immutable revisions, idempotency keys, expected revisions, dry-run, jobs, cancellation, checkpoints, and event streaming.

Required resources include:

~~~text
GET  /v1/stateful-collections
GET  /v1/stateful-collections/{id}
POST /v1/stateful-collections:resolve
GET  /v1/stateful-collection-revisions/{id}

POST /v1/stateful-capture-plans:compile
GET  /v1/stateful-capture-plans/{id}
POST /v1/stateful-captures
GET  /v1/stateful-captures/{id}
POST /v1/stateful-captures/{id}:cancel
POST /v1/stateful-captures/{id}:reconcile

GET  /v1/database-base-backups
GET  /v1/database-log-segments
GET  /v1/database-log-continuity
POST /v1/database-log-continuity:verify
POST /v1/database-recovery-targets:resolve

GET  /v1/virtual-machine-disks
GET  /v1/virtual-machine-snapshot-chains
POST /v1/virtual-machine-snapshot-chains:verify
GET  /v1/virtual-machine-portability-reports

POST /v1/stateful-restore-plans:compile
POST /v1/stateful-restores
GET  /v1/stateful-restores/{id}
POST /v1/stateful-restores/{id}:validate
GET  /v1/stateful-validation-records/{id}
~~~

API behavior:

- Resolve endpoints cannot accept capture or source-mutation options.
- Capture creation requires an immutable plan, source revision, profile digest, objective revision, capability grants, and destination references.
- Responses expose native positions through tagged schemas and never coerce them to one numeric field.
- Continuity endpoints expose gaps, forks, affected targets, last validated contiguous position, and source-retention urgency.
- VM chain endpoints expose complete ancestry and missing parents without returning secret-bearing configuration or memory content.
- Validation requests identify an isolated environment and exact `Processor.VALIDATE` profiles. They cannot request production-network access through an untyped boolean.
- Ordinary API roles cannot retrieve plaintext secrets, memory pages, database pages, VM disks, or raw credential-bearing configuration.
- `RW-MVP-1` returns a profile-gated unsupported response for stateful capture, PITR, guest quiesce, VM conversion, isolated boot, and stateful validation operations.

## 22. Lifecycle, retention, migration, and garbage collection

### 22.1 Qualification lifecycle

Every engine-version and hypervisor-version profile follows the common qualification states:

- `EXPERIMENTAL_DUAL_WRITE`
- `QUALIFIED`
- `DEPRECATED_WRITE_BLOCKED`
- `MIGRATION_REQUIRED`
- `READ_ONLY_LEGACY`
- `RETIRED_NO_DEPENDENTS`

Qualification is scoped to capture form, source and destination versions, operating system, architecture, filesystem or datastore, consistency level, recovery target, and `Processor.VALIDATE` profile. Qualification for one PostgreSQL major version, Oracle incarnation behavior, VMware machine type, QEMU device model, or Hyper-V generation does not transfer to another.

### 22.2 Dependency-root rules

Retention and garbage collection treat as roots:

- Every retained database base backup and the exact journal intervals needed by its accepted recovery targets.
- Timeline, incarnation, recovery-fork, history, control, label, manifest, checksum, configuration, role, extension, external-dependency, and key records.
- Every retained VM disk baseline, delta ancestor, configuration, firmware, NVRAM, vTPM, memory, attached-volume, converter, and key dependency.
- Active capture, restore, validation, repair, rebase, conversion, migration, and reconciliation attempts.
- Legal holds, grace periods, source-retention safeguards, and failed-chain evidence.
- Required historical readers, engine binaries, hypervisor tools, profiles, schemas, and conformance vectors.

GC must compute liveness from signed manifests and complete dependency graphs. Backend listing, engine catalog expiry, hypervisor snapshot age, or apparent unreferenced filenames cannot establish collectability.

### 22.3 Rebase, consolidation, and migration

- A new database base backup does not make earlier bases or logs collectible until every retained target has an alternative validated closure.
- WAL or log compaction must preserve exact replay semantics and engine-native boundaries; otherwise it is a new experimental representation.
- VM snapshot consolidation, disk flattening, changed-block rebase, and format conversion create new immutable representations.
- Old chains remain until the new closure passes cryptographic, structural, and isolated restore validation and the retirement plan commits.
- Profile, engine, hypervisor, firmware, extension, or key upgrades never silently reinterpret historical captures.
- Key rotation retains every required decrypt or unwrap path until all dependent captures are migrated and restore-tested.

### 22.4 Continuous health

Protection health includes:

- Age of the newest qualified base backup or full VM baseline.
- Latest durable contiguous database log position and measured RPO lag.
- Database log gaps, timeline forks, archive-agent failures, and source-retention exhaustion risk.
- VM snapshot-chain depth, missing parents, invalid change-tracking epochs, and unverified consolidation.
- Age and result of the last clean PITR, VM boot, native resume, converted boot, and application-health drill required by policy.
- Availability of readers, converters, engines, extensions, firmware, keys, external objects, and isolated validation capacity.

Upload completion without these results cannot report the stateful collection as healthy.

## 23. Failure and reconciliation semantics

Required explicit failure states include:

- `CAPTURE_PROFILE_UNSUPPORTED`
- `SOURCE_IDENTITY_CHANGED`
- `COLLECTION_INCOMPLETE`
- `BARRIER_FAILED`
- `SOURCE_RESUME_UNCONFIRMED`
- `BASE_BACKUP_INCOMPLETE`
- `LOG_GAP_DETECTED`
- `LOG_LINEAGE_CONFLICT`
- `RECOVERY_TARGET_UNREACHABLE`
- `SNAPSHOT_PARENT_MISSING`
- `CHANGE_TRACKING_INVALIDATED`
- `GUEST_THAW_UNCONFIRMED`
- `MEMORY_STATE_INCOMPATIBLE`
- `FIRMWARE_OR_KEY_BLOCKED`
- `ATTACHED_COMPONENT_MISSING`
- `PORTABILITY_BLOCKED`
- `ISOLATION_UNPROVEN`
- `APPLICATION_HEALTH_FAILED`
- `VALIDATION_PARTIAL`

Reconciliation must determine source state before retrying a backup-mode transition, guest freeze, hypervisor snapshot, log-retention change, snapshot consolidation, or restore publication. It preserves every observed source response and staged object, fences stale attempts, and never guesses whether a source-side mutation occurred.

A source-resume or guest-thaw uncertainty is an operational emergency independent of whether captured bytes are usable. It blocks another mutating attempt until an authorized operator or profile-defined reconciliation confirms safety.

## 24. Delivery phase boundaries

### Phase 0: Report-only planner

- Optional read-only detection may identify likely databases, VM disks, configuration, and unresolved dependency risk.
- No engine backup mode, log archival, hypervisor snapshot, guest quiesce, pause, conversion, restore, or execution is allowed.
- Findings cannot reduce exact filesystem protection.

### Phase 1: `RW-MVP-1` managed-archive exact executor

- All stateful capture features in this document remain excluded from the MVP release gate.
- Ordinary exact repository placement may preserve database and VM files under the generic filesystem profile, but RestoreWeave claims no PITR, native database consistency, guest quiesce, complete snapshot chain, VM bootability, or application health.
- The API rejects stateful mutation and validation operations as unsupported by the active compatibility profile.

### Phase 2: Catalog and controlled prototypes

- Read-only stateful collection resolution, capture-plan simulation, dependency reporting, and laboratory fixtures may ship behind an experimental profile.
- Capture adapters, source mutations, log-retention advancement, and production VM boot remain disabled unless an explicitly experimental development environment is outside the qualified release surface.
- No prototype result may become the sole recovery path or authorize source deletion.

### Phase 3: Qualified stateful capture

- At most one database engine and one hypervisor profile should qualify first, each with a narrow version and platform matrix.
- The database profile must provide physical base backup, continuous ordered-journal capture, gap detection, one exact PITR target form, global and extension inventory, and clean isolated restore validation.
- The VM profile must provide complete configuration and disk capture, snapshot ancestry validation, bounded guest quiesce, attached-volume accounting, isolated cold boot, and application-health evidence.
- Stronger multi-database barriers, cross-hypervisor conversion, memory resume, passthrough devices, and device-bound keys remain blocked or experimental unless separately qualified.

### Phase 4 or later: Expanded stateful profiles

- Additional engines, hypervisors, cloud control planes, versions, architectures, cluster-consistency groups, and external-object adapters.
- Qualified incremental chains, automated rebasing, cross-hypervisor portability, native resume, memory-state recovery, and hardware-security dependencies.
- Storage optimization only after dual-write, corruption injection, long-chain, migration, and repeated clean-room recovery evidence.

## 25. Acceptance tests

A stateful profile cannot become `QUALIFIED` until every applicable test passes and appears in the signed release applicability matrix.

### 25.1 Common plugin and lifecycle tests

1. A collection-resolution `Processor` cannot invoke backup, snapshot, freeze, pause, retention, restore, or execution capabilities.
2. A `CaptureDriver` cannot certify its output or publish without independent `Processor.VALIDATE` records and the normal signed publication protocol.
3. A `Processor.VALIDATE` capability cannot use source-write credentials or reach a production destination.
4. Crash injection at every capture transition leaves the source resumed or produces an immediate `SOURCE_RESUME_UNCONFIRMED` or `GUEST_THAW_UNCONFIRMED` incident with a usable emergency action.
5. A superseded or cancelled worker cannot mutate the source, checkpoint, validate, or publish after fencing expires.
6. Missing, conflicting, unsupported, encrypted, or inaccessible components prevent a complete collection claim and remain exactly protected when possible.
7. Ordinary manifests, APIs, logs, events, diagnostics, and validation artifacts reveal no plaintext secrets or memory content.
8. GC retains every base, journal interval, disk ancestor, reader, key, profile, active job, legal hold, and validation dependency required by a retained manifest.
9. A profile upgrade creates a new resolution and qualification scope and never reinterprets historical evidence.
10. When the objective requires two failure-independent placements, loss of either one does not remove the accepted recovery target for the protected stateful collection.

### 25.2 Database tests

11. A physical base backup restores on a clean isolated host using only its declared portable dependency closure.
12. Corruption or truncation of any base-backup component is detected before publication and during restore.
13. Removing one required WAL, redo, binlog, or transaction-log interval produces `LOG_GAP_DETECTED`, reports the last recoverable native position, and blocks targets beyond it.
14. Overlapping non-identical log segments, timeline forks, reset-log events, incarnation changes, and same-identity conflicts cannot be ordered silently.
15. Native start, end, and replay-required boundaries are tested at the first and last transaction around the base-backup barrier.
16. PITR stops at the requested engine-native target and proves that transactions immediately before and after the target have the expected visibility.
17. A wall-clock PITR request retains the resolved native position and does not claim success when clock ambiguity prevents an exact target.
18. RPO health degrades from the newest independently readable contiguous position even when later segment uploads exist beyond a gap.
19. Source log deletion or slot advancement is blocked until placement, continuity, replay drill, retention, hold, and fencing gates pass.
20. Roles, ownership, grants, scheduled jobs, extensions, binaries, configuration, locale, collation, and encryption dependencies restore or produce explicit component failures.
21. A missing external table, object-store object, linked service, LOB path, or key never disappears from the recovery result and cannot be counted as passing.
22. Successful engine startup with a corrupt index, missing extension, failed invariant, or wrong recovery target does not produce aggregate success.
23. Replica-sourced capture detects stale replay, wrong primary lineage, and an unobtainable required log interval.
24. Multi-volume capture fails the stronger consistency claim when one required volume misses the barrier or exceeds allowed skew.

### 25.3 Virtual-machine tests

25. Removing or replacing any disk parent, overlay, backing file, or change-tracking baseline produces an invalid chain and blocks dependent restore targets.
26. A change-tracking epoch reset forces a new full baseline and cannot reuse the prior bitmap silently.
27. Snapshot-tree divergence and same-name snapshots with different ancestry remain distinct and cannot be merged by display name.
28. A guest-agent timeout, partial filesystem freeze, failed application hook, or uncertain thaw blocks `APPLICATION_QUIESCED` and raises a source-safety incident where applicable.
29. Every required disk and attached volume participates in the declared barrier or appears as an explicit missing or skewed component.
30. A complete cold restore boots in a default-deny isolated environment with production networks, storage, metadata endpoints, schedulers, replication, and cloud sync disabled.
31. Boot success without required guest services or application invariants produces `APPLICATION_HEALTH_FAILED` or `VALIDATION_PARTIAL`.
32. UEFI variables, NVRAM, Secure Boot state, vTPM state, VM encryption keys, and device dependencies restore or produce explicit blocked components.
33. A memory-state capture resumes only on its qualified CPU, machine-type, firmware, disk-barrier, and device profile; incompatible state cannot fall back silently to a native-resume claim.
34. Cross-hypervisor conversion records every configuration change and passes disk, filesystem, boot, guest-agent, and application validation profiles before a portability outcome is accepted.
35. Unsupported passthrough devices, dongles, GPUs, host paths, and device-bound activation remain visible and cannot be dropped automatically to make a VM boot.
36. Snapshot consolidation or disk flattening preserves the old chain until the new self-contained representation passes an independent clean restore.
37. Temporary collision-safe validation identities prevent duplicate MAC, IP, hostname, cluster, and application identity from reaching production while preserving source identities as evidence.
38. Malicious disk, firmware, memory, extension, hook, or guest content remains contained within declared parser, converter, and dynamic-validation resource limits.
