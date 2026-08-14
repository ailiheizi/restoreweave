# Protection Policy and Planning Requirements

> **Profile status:** This document preserves the broader optimization, retention, drill, legacy bootstrap, and enterprise policy model. The MVP planner and exact-fallback contract are defined by [Product Requirements](product-requirements.md), [Core Kernel and Interface Requirements](core-kernel-and-interface.md), and [MVP and Operator Contract](mvp-and-operator-contract.md). `RW-MVP-1` is a single-node Linux/NAS managed archive over one local or mounted root, an honest generic capture profile, one mature exact deduplicating and compressing repository engine reported as one placement, deterministic suffix-then-magic identification, bounded default metadata/text processing, baseline search, CLI plus read-only MCP, and a portable signed RRF namespace and publication commit. Platform- and engine-specific implementations qualify independently. Capsule, Seed, witness, multi-repository, remote REST, semantic-vector, destructive-retention, and enterprise-availability rules below are inactive unless a later named profile activates them.

## 1. Purpose

RestoreWeave must optimize the smallest recoverable set only after it has compiled one explicit protection objective for every protected component. Recovery fidelity alone is insufficient: the planner also needs time, history, availability, independence, immutability, cost, and drill requirements.

This document defines:

- The first-class `ProtectionObjective`.
- Deterministic policy composition.
- Retention and tombstone semantics.
- Asset and entity identity resolution.
- Minimum-recovery-set planning.
- Cost and risk accounting.
- Protection-health derivation.
- Scheduling and drill coverage.

## 2. ProtectionObjective

A `ProtectionObjective` is an immutable published record scoped to a workspace, dataset, entity, asset, component, or concrete selector expansion. It contains:

- Objective ID, revision, canonical digest, state, author, and publication time.
- Scope and the exact snapshot membership used for simulation or approval.
- Recovery-contract reference and component-specific acceptable outcomes.
- Capture-consistency and filesystem-restore requirements.
- CaptureDriver profile, required capture-set atomicity scope, and permitted failure behavior.
- Recovery point objective, expressed as maximum acceptable data age.
- Recovery time objective and the event at which timing begins.
- Restore priority and dependency order.
- Retention horizon and version-history rule.
- Tombstone recovery window and source-absence behavior.
- Minimum complete replica count and failure-independence constraints.
- Required immutability or deletion-delay class.
- Source-revalidation, readback, scrub, and restore-drill cadence.
- Source identity-transition, journal-reconciliation, change-anomaly, and preservation-hold rules.
- Required offline dependency closure.
- Key-recovery quorum, custodian independence, credential-recovery, and ceremony cadence.
- Service monitoring and independent dead-man requirements where the deployment profile claims them.
- Storage, transfer, egress, compute, and operator-time budgets.
- Jurisdiction, residency, privacy, network, and rights constraints.
- Ordered fallback and promotion behavior.
- Risk acceptance and approval references.

An objective is invalid if any required field is implicit, depends on a mutable default that is not version-bound, or refers only to an aggregate dataset outcome while a protected component has no explicit contract.

## 3. Objective states

Required states are:

- `DRAFT`
- `VALIDATED`
- `SIMULATED`
- `PUBLISHED`
- `ACTIVE`
- `SUPERSEDED`
- `ROLLED_BACK`
- `REVOKED`
- `BLOCKED_UNSATISFIABLE`

Publishing creates an immutable version. Activation binds the version to a policy epoch and concrete target scope. Rollback activates a prior immutable version; it does not edit history.

An unsatisfiable objective must identify the exact conflicting requirements, affected components, missing capabilities, and safe preservation behavior. Unsatisfied objectives preserve or promote exact bytes and never silently relax themselves.

## 4. Deterministic policy composition

Policies compose in the following scope order:

~~~text
system safety floor
-> workspace
-> dataset
-> entity or collection
-> asset
-> component
-> one-time action-specific signed operational approval
~~~

Composition rules:

1. A safety floor, legal hold, explicit deny, or stronger parent constraint cannot be weakened by a more specific ordinary rule.
2. A more specific rule may strengthen protection without additional approval.
3. Weakening a parent objective requires a separately scoped signed approval that identifies the exact affected components and concrete membership.
4. Equal-specificity conflicts are resolved only by an explicit deterministic priority and immutable tie-break rule. If no rule applies, the result is blocked and exact preservation wins.
5. Dynamic selectors are evaluated against a named complete scan generation. An approval for current members does not automatically authorize future members unless it explicitly grants that authority.
6. Negative selectors and exclusions never convert an unobserved, unreadable, unsupported, changing, or conflicted item into an omission candidate.
7. Policy evaluation must not depend on database row order, wall-clock race, driver or processor enumeration order, model sampling, or unspecified map ordering.

A scoped execution authority may permit an already bounded class of low-risk actions; it is not another policy layer and cannot change the compiled ProtectionObjective. External proposals, model recommendations, annotations, and search results likewise provide inputs or explanations rather than policy authority. The MVP uses local operator authority and plan digests rather than an `AutomationGrant` resource.

Every compilation emits an `EffectivePolicyRecord` containing:

- All contributing policy versions in evaluation order.
- Selector expansions and membership digests.
- Field-level provenance and winning-rule explanation.
- Conflicts, defaults, and safety-floor applications.
- Required approvals and their exact revisions.
- Final `ProtectionObjective` digest.
- Compiler version and conformance-vector digest.

Every shipped client must expose a field-level `why` explanation before any apply, omission, retention reduction, public network operation, or destructive action. The MVP satisfies this through CLI human and JSON output; later MCP, REST, or UI profiles must preserve the same explanation fields.

## 5. Retention and version history

Retention is a component of the protection objective rather than a storage-backend default.

Supported deterministic rule primitives include:

- Keep the latest `N` committed generations.
- Keep all generations younger than a duration.
- Keep hourly, daily, weekly, monthly, and yearly representatives using a declared timezone and boundary rule.
- Keep event-labeled, manually pinned, legal-hold, release, milestone, and pre-migration generations.
- Keep a minimum history after a source tombstone, ransomware signal, policy change, key event, or backend incident.
- Keep every generation covered by an active `ANOMALY_PRESERVATION_HOLD`, including the policy-required pre-event baseline and anomaly window.
- Keep every generation still needed by a delta base, decoder, model, dictionary, Capsule Core, Recovery Bootstrap Envelope, Recovery Bootstrap Seed, RecoveryHeadWitness record or transition, or restore result.

The retention compiler must declare:

- Evaluation time and trusted-time evidence.
- Candidate generations and the reason each is kept or eligible.
- Deterministic bucket selection and tie-breaking.
- Grace period, deletion approval, and undo window.
- Expected bytes reclaimed and uncertainty.
- Every dependency that prevents retirement.

Clock rollback, missing trusted time, incomplete history, missing manifest shards, active work, an unresolved change anomaly, an active anomaly-preservation hold, or uncertain dependency closure blocks retirement.

## 6. Source absence and tombstone policy

The objective distinguishes:

- Source temporarily offline.
- Source identity changed.
- Path not traversed.
- Permission or parser failure.
- Scan cancelled or incomplete.
- Rename or move.
- Confirmed source deletion.
- Explicit request to purge retained recovery data.

A complete scan-generation comparison may create a tombstone. The tombstone recovery window begins only after the deletion observation becomes authoritative. Temporary source absence does not consume the window.

Before the final version protected only by the tombstone window expires, the system must re-evaluate source availability, legal hold, anomaly signals, replica health, and the objective's minimum history.

Source identity continuity is established only by a signed `SourceIdentityTransitionRecord`. Adoption, replacement, relocation, decommission, or loss cannot be inferred from a matching path, hostname, volume label, UUID, inode history, or equal content alone. A transition that accepts continuity does not transfer a weaker contract, omission approval, source-only treatment, or deletion authority unless those resources independently revalidate against the successor source.

Watcher and change-journal cursors are source-identity and journal-epoch scoped. Overflow, reset, rollback, truncation, provider change, adoption, or replacement forces a new complete baseline; until it commits, source coverage is incomplete and tombstones are prohibited.

## 7. Asset and entity identity resolution

RestoreWeave keeps these identities separate:

- `observation_id`: one path and filesystem-object observation in one scan generation.
- `content_id`: immutable digest-and-length identity of exact plaintext bytes.
- `asset_id`: stable user-facing identity when policy considers observations to be the same evolving asset.
- `entity_id`: a collection or application-level grouping.
- `representation_id`: immutable identity of one representation and provenance binding.

Identity events are append-only:

- `OBSERVED_NEW`
- `RENAMED_OR_MOVED`
- `COPIED`
- `CONTENT_REPLACED`
- `SPLIT`
- `MERGED`
- `RESOLVER_LINK_PROPOSED`
- `RESOLVER_LINK_REJECTED`
- `USER_CORRECTED`
- `IDENTITY_CONFLICT`

Rules:

- Equal content does not automatically mean one asset; independent copies may have different ownership, path, sensitivity, or lifecycle.
- A rename or move may preserve an asset ID only when source identity, history, and conflict checks pass.
- Inode or file identifier reuse is never sufficient by itself.
- Cross-volume moves, split, merge, and collection-resolver disagreements require explicit recorded evidence.
- User corrections create a new identity-resolution version and invalidate affected plans, omissions, and selector approvals.
- An uncertain identity relationship may increase protection but cannot transfer a weaker contract or omission approval.

The MVP may use conservative path-plus-observation identity and propose cross-scan links for review. It must not automatically carry source-only or non-exact treatment across an ambiguous identity transition.

## 8. Minimum recovery set

The planner solves:

~~~text
minimize retained storage + transfer + compute + operational cost
subject to every ProtectionObjective under every required failure scenario
~~~

Required failure scenarios may include:

- Loss of the source host.
- CaptureDriver failure or inability to reproduce the declared consistency boundary.
- Loss or corruption of one storage backend.
- Loss of one provider account, region, site, operator, delete credential, or network path.
- Source artifact disappearance or provider revision drift.
- Loss, compromise, expiry, or revocation of a key or credential.
- Loss of the operational database, identity provider, KMS, DNS, package registry, optional processor registry, or P2P network.
- Decoder, model, runtime, or CPU-architecture obsolescence.
- Ransomware-style deletion and rollback.
- Source-side mass deletion, rename, encryption, or rewrite while the newest bytes still require preservation.
- One corrupted or malicious representation.

The planner must first prove feasibility. It then optimizes using deterministic tie-breaking. It returns:

- Selected representations, sources, recipes, validators, and placements.
- Complete transitive dependency closure.
- Coverage matrix by component and failure scenario.
- Assumptions and evidence age.
- Required human actions and custodians.
- Estimated storage, transfer, compute, time, and monetary cost.
- Residual risk and blocked alternatives.
- A counterfactual explanation of why each excluded option failed.

No average score, global similarity threshold, or aggregate byte-savings target may hide one unsatisfied required component.

## 9. Cost and budget accounting

Every cost estimate records:

- Currency and tax treatment.
- Provider, region, storage class, pricing-source URL or artifact, retrieval date, and validity interval.
- Storage, request, minimum-duration, early-deletion, retrieval, egress, API, compute, GPU, and operator-time units.
- Expected, lower-bound, and upper-bound amounts.
- Assumed compression, deduplication, change rate, readback, drill, and migration volume.
- Recurring budget window and reservation.
- Hard and soft limits.
- Tie-breaking when multiple plans have equivalent policy coverage.

Actual usage is reconciled against the estimate. Material variance invalidates affected optimization decisions and may trigger replanning, but budget pressure never authorizes silent protection reduction.

## 10. Protection health

Health is derived from component evidence, never assigned manually as one opaque dataset flag.

Inputs include:

- Expected-cadence and maximum-snapshot-age status when a saved reference-userland profile declares them; Core itself owns no scheduler.
- Capture-set readiness, achieved consistency, provider receipt, and consumer-receipt agreement.
- Default processor coverage, exact-fallback count, baseline index generation, search coverage, and visible stale or failed extraction state.
- Requested versus currently achievable outcome per component.
- Achieved placement count and failure-domain description without treating one repository as redundant.
- Immutability and deletion-isolation status.
- RRF root, payload receipt, prepared-closure receipt, portable publication-commit validity, recovery-reference freshness, scoped credential availability, and independent trust-anchor availability.
- Authenticated-metadata, sampled-content, full-byte, and clean-machine-drill freshness as separate evidence dimensions.
- Source lifecycle state, complete-baseline status, journal-checkpoint validity, and unresolved identity transitions.
- Key quorum, custodian, control-plane rebuild, witness, Seed, Capsule, and independent-monitor evidence only when a later named profile activates those capabilities.
- Active incidents, legal or anomaly-preservation holds, change anomalies, migrations, and unresolved journals.

Required health states are:

- `HEALTHY`
- `DEGRADED`
- `AT_RISK`
- `BLOCKED`
- `UNRECOVERABLE`
- `UNKNOWN`

An aggregate is the worst required component after dependency propagation. Informational or disposable derivatives cannot improve authoritative recovery health. For `RW-MVP-1`, `HEALTHY` requires a valid portable publication commit in a target outside the source failure domain, current required verification, an independently retained recovery reference, a scoped credential source, an independent trust anchor, and a current clean-install drill that used them. A stale or failed baseline index degrades discovery health but cannot invalidate an otherwise healthy published exact snapshot. Later profiles may add stricter witness, quorum, multi-placement, or custody gates. Every health transition retains reason codes, evidence references, first-observed time, acknowledgement, suppression expiry, escalation, and resolution history.

## 11. Recurrence and drill coverage

Core owns no scheduler. The reference userland may save cadence and health expectations, while scheduler installation, a resident daemon, catch-up policy, and event-triggered orchestration remain later capabilities. A later scheduler profile may support:

- RPO-deadline-driven runs.
- Periodic full scans plus watcher hints.
- Source-mount, reconnect, and change triggers.
- Mandatory full-baseline triggers after source adoption, replacement, identity conflict, or watcher/journal invalidation.
- Dependency chains and prerequisite evidence.
- Priority, preemption, maintenance windows, and restore reservations.
- Catch-up or coalescing after missed windows.
- Explicit behavior when a removable source remains absent.
- Health-watermark publication and independent dead-man deadlines when that managed-service profile is selected.

Drill policies distinguish:

- Candidate restore verification before publication.
- Metadata-only validation.
- Sampled object readback.
- Sampled component restore.
- Full clean restore.
- Clean-machine restore from an independently retained recovery reference.
- Post-publication offline bootstrap restore only for a later profile that activates the legacy bootstrap corpus.
- Destructive-failure and ransomware simulation.

Every verification or drill records its explicit type, selection method, coverage, excluded components, environment, network policy, exact CaptureSetRecord where capture is tested, credential-source and trust-anchor class without secret material, manual steps, timing boundaries, failure injections, and retained evidence. Sampled evidence cannot claim full-byte verification or complete recoverability. RTO begins at the objective's declared incident or operator trigger and includes required human, credential, download, reconstruction, validation, and publication time.

The former `POST_PUBLICATION_BOOTSTRAP_DRILL` and `COLD_VERIFIED` ceremony is retained only as a deferred enterprise-profile concept. It is not an RW-MVP-1 drill or health requirement.

## 12. Later-profile API projection

`RW-MVP-1` does not require REST and exposes planning through the Core Command ABI and CLI, with qualified read-only inspection through MCP. If a later remote or multi-user profile projects the policy model through REST, its resource families may include:

- `/v1/protection-objectives`
- `/v1/policy-versions`
- `/v1/effective-policies`
- `/v1/retention-rules`
- `/v1/identity-resolution-events`
- `/v1/source-identity-transitions`
- `/v1/source-journal-checkpoints`
- `/v1/capture-sets`
- `/v1/change-anomalies`
- `/v1/anomaly-preservation-holds`
- `/v1/key-recovery-policies`
- `/v1/key-recovery-ceremonies`
- `/v1/service-level-objectives`
- `/v1/service-health-watermarks`
- `/v1/cost-models`
- `/v1/protection-health`
- `/v1/health-events`
- `/v1/drill-policies`
- `/v1/agent-proposals`
- `/v1/review-plans`
- `/v1/automation-grants`

Draft, validate, simulate, publish, explain, activate, rollback, and compare operations are distinct. Simulation never mutates storage or policy state.

## 13. MVP profile

The first implementation supports:

- One local workspace and one administrator.
- One configured local or NAS-mounted filesystem root on a single-node Linux/NAS deployment.
- One honest generic capture profile that records either retained immutable snapshot evidence or a validated-live basis with per-entry mutation handling and no false atomicity claim.
- One mature exact deduplicating and compressing repository engine named and qualified by the release applicability matrix, reported truthfully as one placement. Restic, Kopia, Borg, and other engines remain candidates rather than a preselected product contract.
- An onboarding recommendation that places the repository on a separate machine, remote target, or otherwise outside the source failure domain; same-failure-domain use is test-only.
- One exact objective for every readable selected item unless a human decision or already published policy explicitly excludes that item. Excluded bytes are non-recoverable and never count as protected or verified coverage.
- Conservative suffix and magic identification; unknown, unsupported, or optional-processor-failed readable bytes fall back to exact protection plus a warning.
- Bounded default metadata and extracted-text processors for qualified common formats, with a generic exact route for every other file.
- Exact hashing, duplicate grouping, distinct logical-path accounting, and separately reported repository compression, deduplication, index overhead, and transfer effects.
- A rebuildable baseline index over paths, types, checksums, duplicate groups, declared metadata, processing state, common media metadata, and extracted text. Search results resolve through the durable subject and namespace model.
- Deterministic plan explanation, human review through `plan.revise`, and measurable storage and indexing analysis.
- CLI human and machine output plus a read-only MCP adapter. No embedded AI, embedding or CLIP provider, WebUI, scheduler, recurring daemon, or REST service is required.
- A portable RRF companion closure whose signed `PublicationCommitRecord` binds the payload receipt, prepared-closure receipt, plan digest, capture or applied-inventory digest, and authenticated-metadata evidence.
- Clean exact recovery from the target, an independently retained recovery reference, a scoped credential source, an independent trust anchor, and a compatible reader without the operational catalog, baseline index, processor registry, or source host.

APFS, ZFS, Btrfs, LVM, and vendor snapshot integrations may supply stronger optional capture profiles. Their availability or failure never determines whether the generic MVP can qualify. Embeddings, CLIP, vector search, hybrid ranking, and multimodal discovery are staged provider profiles over the same subjects and do not redefine recovery authority.

Multitenancy, automated retention reduction, non-exact recovery authority, source deletion, automated destructive garbage collection, multiple placements, cost-driven placement changes, cold media, and enterprise lifecycle policy are outside the MVP.

## 14. Acceptance criteria

1. Reordering policy storage or evaluation input does not change the compiled objective.
2. An unresolved policy conflict preserves exact bytes and reports the field-level conflict.
3. A more specific ordinary rule cannot bypass a system safety floor, legal hold, or stronger parent objective.
4. A dynamic selector approval does not silently apply to a newly discovered member.
5. An incomplete scan or absent removable source creates no deletion tombstone.
6. Ambiguous rename, copy, split, merge, or resolver evidence cannot inherit a weaker contract automatically.
7. Every selected minimum recovery set passes every required failure scenario or is marked unsatisfiable.
8. Cost-estimate inputs and pricing versions are reproducible, and actual variance is visible.
9. Aggregate health cannot be `HEALTHY` while one required component is expired, unavailable, or below policy.
10. A sampled drill is labeled sampled and cannot satisfy a full-restore requirement.
11. Inventory, the one repository receipt, and the RRF root bind the same retained capture digest or exact applied-inventory basis. Only a qualified snapshot profile may claim `CRASH_CONSISTENT`; a generic live profile preserves its weaker consistency evidence.
12. Source adoption, replacement, decommission, or journal reset creates no tombstone or inherited weakening before a signed transition and complete baseline.
13. A qualifying change anomaly preserves new bytes and freezes omission, retention reduction, placement retirement, and garbage collection until signed resolution.
14. Missing scoped credential access, an absent independent trust anchor, or a missing independently retained recovery reference prevents RW-MVP-1 from reporting `HEALTHY` recovery readiness.
15. A later managed-service profile that claims independent monitoring treats a missing or late health watermark as an SLO failure; RW-MVP-1 makes no such daemon or notification claim.
16. An RRF root or prepared closure without a valid portable `PublicationCommitRecord` is not a committed snapshot and cannot count as protected or healthy.
17. Sampled verification, full-byte verification, and a clean-machine restore drill remain distinct claims; one never silently satisfies another.
18. Baseline search coverage and index freshness are visible, index rebuild changes no recovery record, and every search hit resolves through the same durable subject and namespace used for browse and restore.
19. Failure of any optional platform snapshot profile blocks only that profile and cannot block the generic Linux/NAS MVP.
