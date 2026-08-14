# Release Qualification and Traceability Requirements

> **Profile status:** This is an extended qualification catalog. The first release gate is the single-node Linux/NAS managed-archive profile in [MVP and Operator Contract](mvp-and-operator-contract.md): one local or mounted root, an honest generic capture profile, one mature exact deduplicating and compressing repository engine, deterministic identification, bounded default metadata/text processing, durable whole-subject tags and notes, baseline search, a portable namespace and signed RRF publication commit, bundled read-only Linux FUSE, CLI, and read-only MCP. Platform- and engine-specific implementations qualify separately and are never global gates. REST, WebUI, resident scheduling, embedded AI, embeddings, CLIP, vector or multimodal search, multiple placements, A2A, P2P, public plugin protocols, alternate fidelity, multitenancy, cold media, and enterprise custody are later profiles.

## 1. Purpose

RestoreWeave must not describe a capability as supported merely because code, configuration, or documentation for it exists. A supported capability requires a reproducible chain from the governing requirement to implementation, tests, retained evidence, a declared compatibility profile, and a signed release decision.

This document defines that chain. It applies independently to exact storage, external retrieval, P2P, search, semantic annotations, enrichment providers, AI proposal and automation surfaces, MCP and A2A adapters, media validation, application-aware capture, codecs, `RepositoryDriver` implementations, and every later optional capability.

## 2. Requirement baseline

Every product release binds one immutable `RequirementBaselineRecord` containing:

- Product and schema version.
- Canonical digest of every normative requirements document included in the baseline.
- Stable references for applicable product-level functional requirements.
- Accepted architecture decisions and compatibility profiles.
- Explicitly deferred, experimental, prohibited, and out-of-scope capabilities.
- Creation time, signing identity, signature, and predecessor baseline.

A detailed requirement reference contains document path, heading anchor, statement digest, and baseline digest. Existing stable identifiers such as `FR-01` remain aliases, but an alias alone is insufficient after the underlying text changes.

Changing a normative statement creates a new baseline. Historical releases retain their original baseline and readers.

## 3. Capability states

Every externally visible capability has exactly one release-scoped state:

| State | Meaning |
| --- | --- |
| `SUPPORTED` | The declared profile passed all applicable release gates and has retained signed evidence. |
| `EXPERIMENTAL` | The capability is opt-in, isolated, cannot weaken authoritative recovery, and has explicit limitations and rollback. |
| `DEFERRED` | Requirements may exist, but the release does not implement or expose the capability. |
| `UNSUPPORTED` | The exact capability or compatibility tuple is outside qualified evidence or is not implemented in the release. |
| `PROHIBITED` | The release blocks the capability because its safety, rights, privacy, portability, or correctness contract is not satisfied. |
| `RETIRED` | New use is blocked; historical read or restore support remains for retained dependents. |

Unknown combinations default to `UNSUPPORTED`, even when each component is supported separately. The UI, CLI, API, agent tools, manifests, and reports must use the same state and limitation text.

These are release-scoped capability states, not post-release qualification, protection, or operational health states. `AT_RISK` may belong to `QualificationHealth` or another health model, while `DEGRADED`, `BLOCKED`, and `UNRECOVERABLE` belong to protection or operational health; none silently rewrites an immutable `ReleaseDecision`.

## 4. Compatibility profile

A signed `CompatibilityProfile` binds:

- Host operating system, architecture, kernel, resolver facilities and policy, and required system facilities.
- Source and destination filesystem types and the exact fidelity subset claimed.
- Scanner and root-binding schema, retained-root and descriptor-relative resolver profile, `CaptureDriver`, `CaptureSetRecord` schema, snapshot facility and protection or hold lifecycle, source and mount identity lifecycle, nested-mount policy, change-watcher or journal coverage, loss behavior, and required revalidation points.
- Control database and migration range.
- Backup engine, repository format, adapter, and supported version range.
- Storage backend, protocol, region or topology assumptions, and tested limits.
- Plugin, parser, codec, model, runtime, driver, and accelerator constraints.
- RRF, portable `PublicationCommitRecord`, recovery-reference, and compatible reader versions. Capsule, Seed, Envelope, Witness, and portable-package versions apply only to later profiles that activate them.
- Network profile, privacy mode, and protocol features where retrieval or P2P applies.
- Northbound protocol and specification revision, tool catalog, authentication mapping, and canonical Core Command equivalence where an external client applies. `RW-MVP-1` qualifies CLI and read-only MCP only; mutation grants, REST, and agent lifecycle are later or experimental.
- Linux FUSE adapter implementation, mount-policy profile, required and prohibited mount options, principal and export-root binding, inode and directory-cookie policy, raw-name and sparse-file semantics, cache and revocation profile, and tested kernel and library versions where the bundled mount applies.
- Default processor profiles, baseline index backend and generation, extraction coverage, subject resolution, rebuild behavior, authorization mapping, the MVP whole-subject tag and plain-text note schema, and the authenticated portable annotation-bundle profile for MVP metadata, text, tag, and note search. Richer annotation schemas, embedding spaces, vector indexes, and semantic purge behavior apply only where later semantic search is qualified.
- Maximum tested files, bytes, path depth, container expansion, concurrent jobs, and restore scale.
- Requested and achievable recovery, filesystem, and capture-consistency claims.
- KeyRecoveryPolicy, custodian or recipient independence, repository-credential recovery, and ceremony profile.
- Service-SLO, health-watermark, independent dead-man, notification-delivery, and telemetry profile.
- WCAG 2.2 AA browser and assistive-technology matrix whenever a WebUI capability is supported.

Compatibility is a tuple rather than a collection of independent checkboxes. A release must not infer support for an untested tuple from overlapping partial profiles.

## 5. Traceability graph

The release system maintains an authenticated directed graph:

~~~text
RequirementBaseline
-> RequirementRef
-> CapabilityProfile
-> ImplementationArtifact
-> TestSpecification
-> TestExecution
-> QualificationEvidenceRecord
-> ReleaseDecision
~~~

Every applicable requirement must reach at least one test or an explicitly justified inspection or analysis record. Every supported capability must reach all of its applicable requirements, threats, compatibility constraints, migration obligations, and operator documentation.

The graph must detect and block:

- Applicable requirements with no evidence.
- Tests that no longer bind the current implementation digest.
- Evidence produced against a different compatibility profile.
- Supported features whose negative or failure-path tests are missing.
- Release notes or UI claims broader than the signed capability profile.
- Historical readers or migration paths with no retained qualification evidence.

## 6. Implementation artifacts

An `ImplementationArtifact` record may refer to source, binary, container, WASM module, model, dictionary, schema, migration, configuration bundle, or an implementation of a qualified extension seam such as `RepositoryDriver`. It records:

- Content digest and build identity.
- Source revision and reproducible-build status.
- Compiler, runtime, dependencies, and SBOM.
- Signature, publisher, trust state, and revocation state.
- Applicable license and notice material.
- Exposed capabilities and required privileges.
- Historical decode or restore obligations.

A mutable tag, branch, package range, model alias, or download URL cannot identify a qualified artifact.

## 7. Required test classes

Each capability declares the applicable test classes and why any class is not applicable:

- Unit and schema tests.
- Property tests for canonicalization, identity, policy composition, manifests, and state machines.
- Differential tests against independent parsers, engines, hashes, codecs, or protocol implementations.
- Golden-vector and cross-version compatibility tests.
- Fuzz and adversarial-input tests.
- Crash, cancellation, retry, stale-worker, and fault-injection tests.
- Corruption, rollback, fork, missing-dependency, and hostile-backend tests.
- Security, authorization, privacy-egress, and secret-handling tests.
- Restore tests beginning from a clean environment.
- Portable publication-commit discovery, namespace reconstruction, and clean-install RRF recovery tests for `RW-MVP-1`. Seed, Witness, Envelope, Capsule, and control-plane rebuild tests apply only to later profiles that activate those records.
- Migration, upgrade, downgrade, retirement, and historical-reader tests.
- Scale, performance, resource-quota, and backpressure tests.
- Interoperability tests using an independent implementation where the format is intended to be portable.
- Statistical qualification for learned detection, retrieval, or validation.
- Rights, legal-hold, and non-waivable policy-path tests where distribution or deletion applies.
- Capture-provider, source-transition, journal-reset, deterministic change-anomaly, and anomaly-preservation-hold tests.
- Capture-root and ancestor substitution, root or mount replacement, remount, snapshot-handle substitution, nested-mount policy, retained-handle lifetime, safe FIFO and device-like type pinning, and watcher overflow, rollback, truncation, reset, non-recursive coverage, and uncertain-continuity tests.
- Read-only Linux FUSE qualification covering raw names, hard links, sparse files, symlinks, collision-resolved stable inodes, directory-cookie scope, optional `READDIRPLUS`, exact `EROFS` behavior for every write-capable open and mutation opcode, required and prohibited mount options, cache identity, authorization expiry and revocation through open handles, page cache, and `mmap`, and clean and crash-driven unmount.
- Scoped repository credential recovery, independent trust-anchor, and secret-non-disclosure tests for `RW-MVP-1`; key-share, custodian, and threshold tests only where a later custody profile applies.
- Service-SLO, telemetry-loss, notification-delivery, independent dead-man, and health-watermark replay or rollback tests only where those managed-service capabilities are claimed.
- Automated and manual WCAG 2.2 AA tests for every later supported WebUI critical workflow.
- Cross-interface conformance across only the interfaces named by the capability profile. `RW-MVP-1` covers CLI and read-only MCP; REST, MCP mutation, and A2A are added only by later profiles.
- Baseline search qualification covering metadata, text, tag, and note correctness; subject and namespace resolution; stale and incomplete coverage; index rebuild; processor-generation isolation; and proof that index state cannot change recovery authority or durable annotation revisions.
- MVP whole-subject tag and plain-text note qualification covering schema compatibility, authorized CRUD, optimistic revision checks, tombstones, authenticated portable bundle export and import, loss of SQLite and every index, and rebuild of lexical annotation projections from authoritative records.
- Richer annotation provenance, model-generation isolation, query-time authorization, enrichment egress, and lineage-purge tests where a later semantic catalog applies.

Upload completion, unit-test success, or one same-host round trip cannot substitute for a clean restore test.

## 8. QualificationEvidenceRecord

Every retained execution record binds:

- Requirement, capability, test, implementation, and compatibility-profile digests.
- Test corpus, fixtures, generator seeds, applicable immutable rights-evidence and signed rights-determination references, and privacy records.
- Environment, hardware, filesystem, locale, time source, and network profile.
- Start and completion times.
- Expected result, observed result, stable reason codes, and raw evidence digest.
- Coverage, skipped cases, quarantined cases, warnings, and limitations.
- Reproduction command or machine-readable procedure.
- Actor or automation identity and signature.
- Expiry or invalidation conditions.

Logs may be stored separately, but the evidence record must remain independently authenticatable. Sensitive fixtures, prompts, paths, peer data, and secrets use encrypted references rather than ordinary log fields.

## 9. Failure-scenario qualification

Every protection objective and capability profile maps to explicit failure scenarios. At minimum, applicable releases test:

- Loss or corruption of each single configured repository.
- Loss of the control database and search indexes.
- Loss or compromise of the AI harness, MCP or A2A adapter, enrichment provider, vector database, or model runtime.
- Loss, expiry, or compromise of credentials and key generations.
- Loss of one key custodian or trust service when the declared threshold permits it.
- Loss or compromise of shares until the declared threshold is at risk or unavailable, plus custodian replacement and share refresh.
- Clock rollback and unavailable trusted-time evidence.
- Network partition, DNS failure, provider outage, throttling, and partial reads.
- Source disappearance, incomplete scan, removable-volume absence, and source identity change.
- Source adoption, replacement, decommission, cloned-volume ambiguity, and watcher or journal reset, rollback, truncation, or overflow.
- CaptureDriver failure, stale or substituted snapshot handles, retained-root loss, ancestor symlink or directory substitution, root or mount replacement, bind-mount insertion or removal, remount, nested-mount drift, snapshot hold or deletion-protection loss before consumer start, unavailable required `openat2` or equivalent resolver facilities, live-path fallback, unsafe FIFO or device-like opens, and disagreement between inventory and repository capture-set digests.
- Parser, plugin, codec, model, registry, or original runtime unavailability.
- Concurrent restore, upload, verification, migration, rekey, retention, and garbage collection.
- Ransomware-style deletion and compromised ordinary write credentials.
- Source-side mass deletion, rename, truncation, encryption-like rewrite, entropy change, and anomaly-hold creation or release.
- Destination capacity exhaustion, collision, privilege loss, and mid-restore crash.
- Stale or malicious external sources, trackers, peers, metadata, and web seeds when those capabilities apply.
- Missing, stale, corrupt, rolled-back, or forked Recovery Bootstrap Seed, RecoveryHeadWitness, Recovery Bootstrap Envelope, placement checkpoint, or Capsule Core.
- Source-host, control-service, telemetry-exporter, notification-worker, and independent-monitor failure, including late, replayed, rolled-back, invalid, or non-advancing health watermarks.

The declared failure-independent repositories, peers, accounts, regions, credentials, and administrators must be tested as actual independent failure domains rather than labels.

## 10. Performance and scale baselines

Qualification records separate correctness limits from performance observations. Each supported profile publishes:

- Inventory rate and memory use by file-count and metadata shape.
- Hashing, upload, download, readback, and restore throughput.
- Catalog, manifest-shard, and placement-ledger scale.
- Planning latency and optimizer bounds.
- API and job concurrency.
- FUSE cold and warm first-byte latency, large-directory `readdir` and qualified `READDIRPLUS` scaling, sequential and random-read throughput, repository request and byte amplification, process and kernel-cache memory, concurrent file and directory handle limits, attribute, entry, negative-entry, and data-cache behavior, revocation residual access, and clean and crash-driven unmount time.
- P2P metainfo, active-swarm, peer, and pack-cohort limits where applicable.
- Search-index size, rebuild time, and ACL-propagation delay where applicable.
- Agent-proposal staleness, AutomationGrant revocation latency, dynamic-selector drift, and cross-interface equivalence where applicable.
- CPU, GPU, memory, temporary-disk, and network quotas.
- RPO and RTO test results, including human and key-custody time.
- Service-SLO observations for detection, queueing, watermark, notification delivery, telemetry loss, and reconciliation backlog.
- Accessibility test matrix and critical-workflow completion evidence where the WebUI applies.

Performance regression cannot silently change protection scope, skip verification, weaken parsing, or lower fidelity.

## 11. Statistical and model qualification

Learned detectors, classifiers, embeddings, candidate generators, perceptual validators, and neural codecs require separate evidence for each model and preprocessing profile.

Qualification includes:

- Locked calibration and held-out corpora.
- Corpus provenance, license, consent, privacy, and representativeness.
- Near-miss, adversarial, rare-format, out-of-domain, and multilingual slices.
- Confidence intervals and explicit false-accept limits for acceptance validators.
- Separate discovery recall from recovery-acceptance precision.
- Hardware, precision, runtime, and fallback comparisons.
- Drift and shadow evaluation before model or threshold promotion.

Search usefulness does not qualify recovery authority. A model change creates a new evidence generation and cannot inherit old thresholds without validation.

## 12. Migration and historical recovery

Before releasing a schema, engine, codec, key, backend, manifest, Capsule Core, Recovery Bootstrap Envelope, Recovery Bootstrap Seed, RecoveryHeadWitness, or protocol migration, qualification must prove:

1. Old retained generations remain readable before migration.
2. The migration can resume or reconcile after every crash boundary.
3. New placements or representations pass independent readback and restore.
4. Rollback does not reinterpret already published records.
5. Retirement occurs only after all protected dependents have a verified successor.
6. A clean environment can restore both pre-migration and post-migration generations.

For every retained transformed representation, qualification MUST invoke the pinned `Processor.DECODE_REPRESENTATION` operation using only the encoded representation and pinned dependency closure. The host independently checks decoded length and digest, and separately qualifies declared streaming, range, seek, restart, temporary-space, and minimum-readable-unit behavior through the production `FileAccess` path.

Readers needed only for historical recovery remain `RETIRED` for new work but retained and tested for decode or restore until no protected dependency exists.

## 13. Release decisions and exceptions

A signed `ReleaseDecision` contains the requirement baseline, supported capability profiles, compatibility tuples, evidence-root digest, known limitations, security findings, migration state, release signer, and predecessor release.

An exception:

- Is scoped to one requirement, profile, release, duration, and risk owner.
- Cannot create cryptographic identity, legal rights, distribution authority, privacy consent, or recovery evidence.
- Cannot waive a false exact-recovery result, unauthorized egress, signature failure, rollback or fork detection, path escape, nonce reuse, legal hold, or loss of the last healthy required representation.
- Cannot make an experimental capability count toward the minimum recovery set unless the governing objective explicitly permits it and a conventional qualified fallback remains.

Expired or revoked exceptions invalidate the affected supported claim until requalification.

## 14. Post-release monitoring

Post-release evidence derives a separate `QualificationHealth` of `HEALTHY`, `AT_RISK`, `SUSPENDED`, or `REQUALIFICATION_REQUIRED` without rewriting the historical release decision or its release-scoped capability state. A new signed release or emergency capability decision may later change the release-scoped state to `EXPERIMENTAL`, `PROHIBITED`, or `RETIRED`.

Triggers include:

- Security vulnerability or signer compromise.
- Newly discovered corruption, false acceptance, or restore failure.
- Provider, protocol, license, rights, or compatibility change.
- Expired source, credential, model, validator, Capsule Core, Recovery Bootstrap Envelope, Recovery Bootstrap Seed, RecoveryHeadWitness, or recovery drill.
- Performance regression that threatens the declared RPO or RTO.
- CaptureDriver failure, source-identity or journal drift, capture-set disagreement, or a confirmed source-change anomaly.
- Lost or compromised key shares, custodian or quorum loss, stale recovery ceremonies, or failed repository-credential recovery.
- Missed service SLOs, absent or invalid health watermarks, notification-delivery failure, telemetry loss, or independent-monitor outage.
- A critical WCAG defect in a supported WebUI recovery, approval, key, anomaly, or lifecycle workflow.

The product identifies affected snapshots, blocks unsafe new work, preserves exact data when possible, schedules migration or revalidation, and emits operator-visible remediation deadlines.

## 15. RW-MVP-1 qualification boundary

The `RW-MVP-1` release profile qualifies only:

- A single-node Linux/NAS-oriented controller with one local single-administrator workspace; no one Linux distribution, NAS vendor, or filesystem is inferred beyond the signed compatibility tuples actually tested.
- One configured local or mounted filesystem root.
- One generic `CaptureDriver` profile that reports an immutable snapshot basis when proven or a validated-live basis with explicit per-entry mutation and collection-consistency evidence. Optional APFS, ZFS, Btrfs, LVM, and vendor drivers qualify separately and never gate the generic profile.
- One mature exact deduplicating and compressing repository engine through one qualified `RepositoryDriver`, reported as one placement. The release applicability matrix names the engine and version; every alternative requires its own signed compatibility profile.
- A separate-machine, remote, or otherwise separate-failure-domain target recommendation; same-failure-domain use is explicit test mode only.
- A local operational catalog and baseline search index with independently durable authenticated recovery records. Loss of either projection cannot change publication truth or prevent clean recovery.
- Inventory, the one payload receipt, and the RRF root binding the same retained capture digest or exact applied-inventory basis.
- File-level exact recovery with capture consistency reported at the actually achieved level; only a qualified snapshot tuple may claim `CRASH_CONSISTENT`.
- Exact content identity, duplicate grouping, repository compression and deduplication, and original-path namespace reconstruction.
- Exact fallback plus a warning for readable unknown, unsupported, or optional-processor-failed content.
- Suffix evidence followed by magic-byte evidence, with conflicts retained and every entry routed through either a qualified common processor or the generic exact route.
- Bounded default metadata and extracted-text processing for the declared common-format matrix, including sandbox, quota, provenance, coverage, and failure tests.
- Durable whole-subject tag and plain-text note CRUD, revision history, tombstones, and authenticated portable annotation-bundle export and import independently of the local catalog and every search index.
- Baseline search over paths, types, checksums, duplicate groups, declared metadata, common media metadata, durable tags and notes, processing state, and extracted text, with stable subject and namespace resolution and rebuild qualification.
- Explicit human or already-published-policy exclusions, with excluded bytes reported as non-recoverable and never counted as protected or verified coverage.
- CLI human and machine output plus the qualified read-only MCP profile over the Core Command ABI.
- A bundled read-only Linux FUSE projection over the authenticated original-path namespace, binding one principal, one export root, and one immutable snapshot; verifying `ro,nodev,nosuid,noexec`; refusing `allow_other` and arbitrary mount-option passthrough; preserving qualified raw-name, hard-link, sparse-file, stable-inode, and scoped directory-cookie semantics; and returning `EROFS` for every write-capable open and mutation opcode. Cache, `mmap`, open-handle revocation, and unmount limitations are declared for the exact compatibility tuple.
- A portable original-path namespace and RRF companion closure with a signed `PublicationCommitRecord` that binds its RRF root, payload receipt, prepared-closure receipt, plan digest, capture or applied-inventory digest, authenticated-metadata evidence, generation, and fence.
- Sampled verification, explicit full-byte verification, recovery-reference export, and clean-install exact restore using a scoped credential source and independent trust anchor without the operational catalog, baseline index, processors, plugin registry, or source host.

P2P, source-only omission, non-exact authority, public plugin execution, WebUI, embeddings, CLIP, vector or multimodal search, embedded AI or an agent harness, external enrichment as a recovery dependency, MCP mutation grants, REST, A2A, learned codecs, application-consistency claims, multitenancy, recurring-daemon or scheduler claims, multiple placements, cold media, destructive automatic garbage collection, and `RESTOREWEAVE_PACKS` are `DEFERRED` or `PROHIBITED` in this profile even though later-phase requirements exist.

## 16. Acceptance criteria

1. Every release has one signed requirement baseline and release decision.
2. Every supported capability has a complete trace to applicable requirements, implementation artifacts, tests, and current evidence.
3. Unsupported compatibility tuples fail explicitly rather than inheriting partial support.
4. `RW-MVP-1` passes a clean-install restore beginning with only the qualified repository target, independently retained recovery reference or equivalent exact selector, scoped credential source, independent trust anchor, and compatible reader. A later profile that activates the legacy Seed, Witness, Envelope, or Capsule corpus qualifies that corpus separately.
5. Failure-scenario evidence covers every failure domain claimed by the protection objective.
6. Historical readers and migration paths remain qualified while protected dependents exist.
7. Model or threshold changes cannot inherit recovery authority without new statistical evidence.
8. Exceptions cannot waive non-waivable integrity, rights, privacy, legal-hold, or last-copy requirements.
9. UI, CLI, API, agent, manifest, and release-note claims do not exceed the signed capability profile.
10. All evidence required to reproduce the release decision remains authenticated and recoverable without the ordinary control database.
11. Inventory, the one repository receipt, and the RRF root for every qualified `RW-MVP-1` generation bind the same retained capture digest or exact applied-inventory basis, and no live profile claims snapshot atomicity without evidence.
12. Source adoption, replacement, decommission, loss, and journal invalidation cannot create deletion evidence or inherited weakening before a signed transition and complete baseline.
13. A qualifying deterministic change anomaly preserves new bytes and freezes omission, retention reduction, placement retirement, and GC until signed resolution.
14. `RW-MVP-1` qualification proves one scoped repository credential source and one independent trust anchor remain usable without exposing reusable secret material in ordinary evidence; threshold custody is qualified only by a later profile.
15. Independent dead-man and notification-delivery evidence is required only for a later profile that claims those managed-service capabilities.
16. WCAG evidence is required only for a later profile that ships a supported WebUI capability.
17. Baseline metadata, text, tag, and note search passes correctness, stale-coverage, rebuild, subject-resolution, and namespace-resolution tests, while index loss or compromise cannot create, suppress, or reinterpret a published snapshot or alter durable tag and note revisions.
18. Failure of any optional platform-specific capture tuple affects only that tuple and never blocks the generic Linux/NAS qualification profile.
19. Durable tag and note records survive loss of SQLite and every search index through authenticated portable annotation-bundle export and import, and their lexical projections rebuild without changing revision history.
20. The bundled read-only Linux FUSE projection passes namespace and raw-name equivalence, authorization, single-principal and single-root binding, immutable-snapshot pinning, hard-link and sparse-file fidelity, collision-resolved inode stability, directory-cookie scope, streaming and random reads, required and prohibited mount-option verification, exact `EROFS` mutation rejection, cache and memory bounds, concurrent-handle, revocation and residual-access, large-directory and amplification, and clean and crash-driven unmount qualification for the declared Linux compatibility tuple.
