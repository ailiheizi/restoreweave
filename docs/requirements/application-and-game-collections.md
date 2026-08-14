# Application and Game Collection Requirements

> **Profile status:** Application- and game-aware collection recovery is a staged profile above the NAS-first managed-archive MVP. `RW-MVP-1` may identify these files, preserve them through the generic exact route or exact fallback, and expose safe type and metadata evidence through baseline search. Full collection resolution, application-aware capture, reacquisition, restoration, and execution validation remain later qualified capabilities and do not change the MVP's generic interfaces or its exact-recovery contract based on one mature repository.

## 1. Purpose

RestoreWeave must treat an application or game as a versioned collection of independently protected components, not as one installation directory or one store identifier.

This document defines later-phase requirements for collection resolution, component identity, platform profiles, application-aware capture, external reacquisition, restoration, and validation. It is governed by:

- [Product Requirements](product-requirements.md) for product scope and phase gates.
- [MVP and Operator Contract](mvp-and-operator-contract.md) for the generic exact route, baseline search, and optional read-only `RW-MVP-1` resolver boundary.
- [File Identification and Extraction Requirements](file-identification-and-extraction.md) for evidence, parser coverage, and typed observations.
- [External Source and Retrieval Requirements](external-source-and-retrieval.md) for store-managed payload bindings and quarantine.
- [Extension System Requirements](plugin-system.md) for independently permissioned entry points.
- [Recovery Fidelity Requirements](recovery-fidelity.md) for component outcomes and functional validation.
- [Operations and Lifecycle Requirements](operations-and-lifecycle.md) for capture consistency, migration, and restore drills.
- [Security and Threat Model](security-and-threat-model.md) for hostile executables, credentials, network access, and sandboxing.

## 2. Non-goals

Application and game support does not:

- Infer recoverability from an application name, install path, store listing, or launcher status.
- Treat installed payload, saves, configuration, mods, caches, credentials, entitlement, and activation as one protection class.
- Bypass DRM, licensing, account, device, region, anti-cheat, or access-control restrictions.
- Execute, install, mount, import, launch, patch, or update recovered content by default.
- Assume that cloud synchronization is a backup or that it contains the latest intended state.
- Make the optional collection resolver a dependency of exact managed-archive protection, exact fallback, baseline search over generic fields, or MVP release success.

## 3. Governing invariants

1. Collection resolution produces evidence and membership records; it does not authorize omission, retrieval, execution, or a weaker recovery outcome.
2. Every required component has its own recovery contract, source bindings, dependencies, and validation result.
3. Unknown, unsupported, encrypted, conflicting, partially resolved, or changing state defaults to exact preservation.
4. Store-managed payload is reacquirable only after an immutable source binding, clean cold acquisition, independent verification, rights review, and objective-specific approval.
5. Saves, mods, user-created content, local configuration, and device-specific state are never inferred to be reacquirable from the install payload.
6. Acquisition and restoration do not imply permission to execute.
7. Resolver or platform-profile failure must leave the generic exact route, exact fallback, and baseline search over generic fields available.
8. A collection may overlap other collections. Membership never grants exclusive ownership of an asset or lowers another objective.

## 4. Collection, component, and content identity

RestoreWeave reuses the common identity model:

- `asset_id` identifies a stable user-facing asset across path changes when policy considers it the same item.
- `content_id` identifies one exact plaintext byte sequence by digest and length.
- `representation_id` identifies one immutable representation and its provenance.

Application and game support adds:

- `collection_id`: stable logical identity of one installed application, game, toolchain, launcher-managed product, or user-approved aggregate.
- `collection_revision_id`: immutable snapshot-scoped resolution of membership, roles, provider facts, and dependency edges.
- `collection_component_id`: stable logical role within a collection, such as one save slot, configuration set, mod, runtime, DLC package, or install-payload variant.
- `component_revision_id`: immutable binding from a component role to the exact assets, source bindings, dependencies, and observed versions in one collection revision.
- `profile_subject_id`: privacy-preserving local identity for the applicable operating-system user, application profile, launcher profile, or game account. It must not expose raw account names or credentials in ordinary manifests or logs.

A filesystem path, bundle name, package name, executable name, store product ID, or cloud account ID is evidence, not sufficient collection identity by itself.

Side-by-side editions, branches, architectures, locales, mod sets, profiles, compatibility prefixes, or user accounts remain distinct component revisions unless an explicit domain profile defines their relationship.

## 5. CollectionResolutionRecord

Every collection-resolution processor run returns an attributed proposal containing candidate identities, membership, dependencies, coverage, provenance, conflicts, and a claimed state. The host validates that proposal against the selected profile and publishes the authoritative `CollectionResolutionRecord`. User corrections and acceptance decisions are referenced by the record and are never authored or silently applied by the processor.

The authoritative record contains:

- Resolver entry-point ID, package digest, configuration digest, profile digest, runtime digest, and sandbox-policy digest.
- Source scan generation, host, platform, filesystem, user-profile subjects, and observation times.
- Proposed `collection_id` and immutable `collection_revision_id`.
- Provider, application, product, build, branch, depot, package, DLC, platform, architecture, locale, and edition evidence where available.
- Every component, role, member asset, membership edge, required or optional status, and dependency edge.
- Exact evidence for each membership decision, including manifests, bundle metadata, registry or preference records, database rows, path context, signatures, and parser coverage.
- Conflicts, overlapping collections, missing roots, unsupported fields, permission failures, stale evidence, and unresolved components.
- Whether the run was complete for the declared profile and why.
- User corrections and the immutable decisions that accepted or rejected proposed identity continuity.

Required resolution states are:

- `RESOLVED_COMPLETE`
- `RESOLVED_PARTIAL`
- `CONFLICTING_MEMBERSHIP`
- `STALE_PROFILE`
- `UNSUPPORTED_VERSION`
- `PERMISSION_BLOCKED`
- `SOURCE_INCOMPLETE`
- `UNKNOWN`

Completeness is relative to a versioned platform profile. A resolver must not report complete coverage when it cannot prove the profile's declared roots and membership rules were inspected.

## 6. Component roles and conservative defaults

Required component roles include:

| Role | Meaning | Default treatment |
| --- | --- | --- |
| `INSTALL_PAYLOAD` | Store-, vendor-, package-, or installer-managed program bytes. | Exact until a qualified immutable source binding and cold test permit a scoped alternative. |
| `EXECUTABLE_AND_SIGNATURE` | Executables, libraries, signatures, notarization, and integrity catalogs. | Exact or pinned source artifact; never executed by default. |
| `USER_SAVE` | Saves, checkpoints, profiles, progress, worlds, and local databases. | Exact. |
| `USER_CONFIGURATION` | Preferences, settings, key bindings, UI layout, and local policy. | Exact. |
| `USER_CREATED_CONTENT` | Levels, maps, projects, screenshots, recordings, replays, exports, and authored assets. | Exact. |
| `MOD_OR_PLUGIN` | Mods, extensions, scripts, plugins, patches, and their local changes. | Exact unless independently source-bound and tested. |
| `LOAD_ORDER_AND_RESOLUTION_STATE` | Mod order, enablement, conflicts, dependency solver output, and compatibility state. | Exact or deterministic semantic invariants under a qualified profile. |
| `DLC_OR_ADDON` | Optional or licensed payload and its manifest state. | Component-specific exact or qualified source treatment. |
| `RUNTIME_OR_COMPATIBILITY_LAYER` | Frameworks, redistributables, emulators, Wine or Proton prefixes, virtual runtimes, and drivers. | Exact or pinned source plus tested version constraints. |
| `CACHE_OR_REBUILDABLE_STATE` | Shader, download, thumbnail, index, and derived caches. | Exact unless a producer-aware profile proves safe invalidation and rebuild without losing user state. |
| `LOG_OR_TELEMETRY` | Logs, diagnostics, history, and telemetry. | Exact by default; a scoped policy may reduce protection only after sensitivity and recovery value are reviewed. |
| `CLOUD_SYNC_METADATA` | Sync journals, remote revision IDs, conflict markers, and last-known state. | Exact metadata evidence; not proof that remote state is complete or current. |
| `ENTITLEMENT_EVIDENCE` | Purchase, subscription, license, account, region, or access evidence. | Protect according to sensitivity; evidence does not itself authorize acquisition or guarantee activation. |
| `SECRET_OR_CREDENTIAL` | Tokens, cookies, keys, passwords, passphrases, and private account material. | Exact protected asset or dedicated secret-store reference; never ordinary manifest plaintext. |
| `ACTIVATION_OR_DEVICE_BOUND_STATE` | Machine-bound activation, secure-enclave state, certificates, or hardware-bound keys. | Exact when technically and legally portable; otherwise explicit blocked or manual recovery state. |
| `MANIFEST_AND_PROVENANCE` | Store manifests, receipts, installer metadata, checksums, source identity, and version evidence. | Exact. |

A platform profile may propose a namespaced, versioned component role only when it maps to a host-owned protection class and declares must-understand behavior. Unknown roles retain exact bytes, remain visibly unresolved, and cannot weaken omission, placement, or restore semantics. The host, not the resolution processor, publishes the accepted durable role binding.

## 7. Dependency closure

Every collection revision records a complete component and restore dependency graph.

Required edge kinds are:

- `REQUIRES`
- `OPTIONAL`
- `CONDITIONAL_ON_VARIANT`
- `ALTERNATIVE_PROVIDER`
- `MUST_PRECEDE`
- `MUST_FOLLOW`
- `CONFLICTS_WITH`
- `SHARED_WITH_COLLECTION`
- `GENERATED_FROM`
- `FETCHED_FROM`

The closure may include:

- Assets across multiple directories, volumes, users, containers, bundles, package roots, registries, preference domains, and application databases.
- Executables, libraries, runtimes, DLC, language packs, fonts, drivers, launchers, compatibility prefixes, and plugin hosts.
- Saves, journals, transaction logs, mod-manager databases, load order, configuration, sidecars, and user-created content.
- Source bindings, credentials, entitlement evidence, signatures, decoders, validators, restore tools, and platform-profile dependencies.

Every edge records whether it is required for install, launch, state loading, exact recovery, source reacquisition, validation, or a named optional feature.

Cycles are represented explicitly and restored as a strongly connected group with a declared staging and validation procedure. A cycle must not be broken by silently dropping a member.

No collection-level success is reported while a required component or dependency remains missing, unresolved, stale, or outside its acceptable outcome set.

## 8. Approved seam mapping

Application and game support uses the existing `Processor`, `CaptureDriver`, and later `RetrieverDriver` seams plus core-owned restore execution. Collection, capture, restore, and validation remain distinct capability profiles and authority domains even when one package supplies several implementations.

### 8.1 `Processor.PARSE` and `Processor.ENRICH` collection resolution

A collection-resolution processor profile:

- Reads only declared filesystem and local metadata roots.
- Emits collection, component, membership, and dependency evidence.
- Has no ambient network, secret-read, process-control, installation, execution, retrieval, deletion, or restore capability.
- Cannot mark a component safely omittable or source-qualified.

### 8.2 Application-consistent `CaptureDriver` profile

An application-consistent capture profile may perform a narrowly defined quiesce, export, checkpoint, barrier, snapshot, or thaw workflow. It uses separately granted process-control, filesystem, IPC, credential-reference, and timeout capabilities and returns one `CaptureSetRecord` with namespaced hook, barrier, snapshot, export, and resume evidence under its declared consistency class.

It cannot classify its own output as complete, approve omission, contact an external store through undeclared authority, or validate its own recovery result without an independent `Processor.VALIDATE` profile.

### 8.3 Later `RetrieverDriver`

Store, vendor, package, or publisher acquisition uses a later `RetrieverDriver` under [External Source and Retrieval Requirements](external-source-and-retrieval.md). Retrieval authority, credentials, rights, network scope, quarantine, and cold-test evidence are independent from collection resolution and capture.

### 8.4 Core-owned restore execution

The core-owned restore executor stages components in dependency order and applies only the filesystem, registry, preference, package, or application mutations declared by an immutable profile and restore plan. It may invoke a pinned `Processor.TRANSFORM` capability for a declared conversion, but restore execution is not a separate public extension seam.

Restore execution cannot launch restored content unless a separate dynamic-validation job is explicitly authorized.

### 8.5 `Processor.VALIDATE`

Static and dynamic checks use separately qualified `Processor.VALIDATE` capability profiles. A package may contain several roles only as independently negotiated capabilities with separate permissions, configuration, lifecycle state, approvals, audit records, and conformance tests.

## 9. PlatformProfile contract

Every supported operating-system and ecosystem combination has an immutable `PlatformProfile` containing:

- Profile ID, version, canonical digest, publisher, signature, and support lifecycle.
- Operating-system versions, architectures, filesystems, launcher or store versions, and application manifest versions.
- Known application roots, per-user roots, shared roots, bundle or package metadata, registry or preference locations, compatibility prefixes, cloud-state locations, and special filesystem semantics.
- Stable provider identifiers and their normalization rules.
- Collection identity rules, component-role rules, membership evidence, dependency rules, and completeness criteria.
- Supported install variants, branches, locales, architectures, accounts, profiles, DLC, mod managers, and runtimes.
- Secret-bearing fields and required redaction behavior.
- Capture, source-binding, restore, and validation capabilities.
- Known blind spots, unsupported versions, and safe fallback behavior.
- Golden fixtures and adversarial conformance vectors.

A profile upgrade creates a new resolution generation. It never silently reinterprets historical membership or source qualification.

## 10. Capture and quiesce lifecycle

Application-aware capture follows:

~~~text
preflight
-> prepare
-> quiesce or export
-> establish capture barrier
-> capture required volumes and components
-> validate capture evidence
-> thaw or resume
-> independent component validation
-> publish achieved consistency level
~~~

Required behavior:

- Preflight identifies application version, active processes, profile subjects, open stores, required volumes, hook identities, permissions, temporary space, and maximum interruption time.
- Hooks use signed allowlisted executable identities and structured arguments. Manifest-provided shell commands are never authority.
- Freeze, export, checkpoint, snapshot, and thaw operations have bounded timeouts, fencing, cancellation, and emergency-resume behavior.
- Thaw or resume is attempted after every success, failure, cancellation, timeout, or worker loss; unresolved quiesce state triggers a critical alert and blocks publication of a stronger consistency claim.
- Evidence records start, barrier, snapshot, export, completion, and resume times; process and application versions; volume identities; logs or transaction positions; and cross-volume skew.
- The `CaptureDriver` retains distinct namespaced hook, application-barrier, snapshot, export, and resume receipts inside one capture result. Snapshot success alone cannot mint or imply a stronger application-consistency claim.
- Hook output is hostile until parsed and validated.
- Failure may fall back only to an explicitly acceptable weaker capture-consistency level. Otherwise the collection is blocked or retained as exact crash-consistent filesystem data without a stronger application claim.

## 11. Exact versus reacquirable treatment

Store-managed payload may use `SOURCE_EQUIVALENT` only when:

1. The exact provider, product, build, depot, branch, platform, architecture, locale, edition, and component closure are immutably bound.
2. Expected artifact identities, signatures, and independent hashes are retained where available.
3. A clean acquisition starts without a reusable payload cache and completes under the declared network, credential, rights, and time policy.
4. Every required component passes structural, identity, malware, and application-profile validation.
5. The observed RTO and dependency risk satisfy the protection objective.
6. An exact local fallback remains through approval and grace.
7. Source drift, entitlement failure, provider removal, region change, or validator failure promotes exact local protection while the original remains available.

`SOURCE_EQUIVALENT` does not become `ORIGINAL_BIT_EXACT` unless the acquired complete byte sequence matches the original content identity.

Source qualification for install payload never applies transitively to saves, configuration, mods, user content, secrets, activation, or cloud conflict state.

## 12. Overlapping collections and conflict handling

One asset may belong to multiple collections, profiles, components, or dependency closures.

Requirements:

- Membership is a versioned relation, not exclusive ownership.
- Shared physical content may deduplicate, but every logical membership, role, objective, and restore-order edge remains visible.
- The strictest applicable effective protection objective governs shared content. Resolver confidence cannot weaken it.
- Removing or changing one collection must not create a deletion tombstone for an asset still observed or required elsewhere.
- Conflicting provider identities, profile subjects, editions, or component roles produce `CONFLICTING_MEMBERSHIP` and preserve exact bytes.
- Manual corrections are durable decisions with actor, reason, evidence, and affected revisions. A correction that weakens protection still requires the normal approval lifecycle.
- Split and merge decisions create explicit lineage between collection identities and never rewrite historical revisions.

## 13. Rights, entitlement, secrets, and privacy

- RestoreWeave does not determine whether a license permits copying, reacquisition, modification, export, emulation, or transfer.
- User reproduction authority and source distribution authority remain independent records.
- Entitlement evidence may support a decision but does not authorize network contact, bypass DRM, or prove future availability.
- Region-, account-, subscription-, device-, and time-bound restrictions are explicit dependencies.
- No adapter may extract, log, export, or send credentials, cookies, tokens, private keys, device identifiers, account names, friends lists, cloud-save contents, or telemetry without an exact field-level capability and privacy policy.
- Secret-bearing local files remain protected exact assets unless a dedicated secret-store adapter replaces them with a tested, independently recoverable reference.
- Ordinary manifests, events, diagnostics, prompts, and reports use redacted identities and credential references.
- Cloud or store contact is a separate networked job with destination, field, privacy, residency, rights, budget, and approval disclosure.
- DRM, anti-cheat, kernel drivers, privileged installers, and device-bound activation are never invoked to prove source or collection identity.

## 14. Restore behavior

Application and game restore begins with an immutable requested collection revision and component selection.

The core-owned restore executor must:

1. Run destination and platform-profile preflight.
2. Resolve the complete selected dependency closure.
3. Acquire any approved source components into quarantine.
4. Stage exact and acquired components in an isolated root.
5. Apply namespace, permissions, metadata, registry, preference, package, and profile changes through the recorded restore plan.
6. Keep cloud synchronization, automatic update, launcher repair, post-install hooks, and application execution disabled.
7. Perform static identity and structural validation.
8. Optionally run a separately approved dynamic-validation job.
9. Publish per-component outcomes, capture consistency, filesystem fidelity, unresolved dependencies, and manual actions.

Restore must prevent cloud or launcher synchronization from overwriting staged local state before conflict policy is reviewed. Remote-wins, local-wins, merge, keep-both, and block are explicit recorded decisions; no default remote overwrite is permitted.

Unknown executable content, installers, scripts, mods, plugins, and retrieved payload remain quarantined after restoration until the applicable release policy passes.

## 15. Dynamic validation and execution boundary

Static validation is the default. Launch, state loading, migration, representative queries, or gameplay smoke tests require an independent dynamic-validation job.

Dynamic validation requires:

- Explicit policy and a signed `APPLICATION_DYNAMIC_EXECUTION` approval naming the collection revision, executable identities, test profile, environment, network policy, credentials, time, resources, and expected side effects.
- A disposable VM, hardened container where sufficient, or equivalent isolated operating-system environment with no access to unrelated host data.
- No network by default. Any required provider or license-server access uses allowlisted destinations and scoped temporary credential references.
- Synthetic or narrowly scoped test accounts and fixtures where practical.
- Kernel modules, anti-cheat drivers, privileged services, self-updaters, crash uploaders, telemetry, and arbitrary child execution disabled unless separately reviewed and essential to the declared test.
- Snapshot or rollback of the validation environment after every run.
- Complete stdout, stderr, event, filesystem-mutation, network, crash, and result evidence with sensitive data redacted.
- Per-component PASS, REVIEW, FAIL, or INCONCLUSIVE results under a pinned functional validator profile.

A dynamic pass proves only the declared function contract in the tested environment. It does not establish byte identity, source rights, future provider availability, or undeclared behavior.

## 16. Manifest, API, and WebUI requirements

Authenticated records must include:

- `PlatformProfile`
- `CollectionResolutionRecord`
- Collection, revision, component, and profile-subject identities.
- Membership and dependency edges.
- Component roles and effective protection objectives.
- Capture preflight, hook, barrier, consistency, resume, and validation evidence.
- Store-managed source bindings, cold acquisitions, immutable rights-evidence references, signed rights determinations, credentials, and staleness.
- Restore plans, conflict policies, static and dynamic validation records, and per-component outcomes.

Suggested API resources are:

~~~text
/v1/platform-profiles
/v1/collection-resolution-runs
/v1/application-collections
/v1/application-collection-revisions
/v1/application-components
/v1/application-capture-jobs
/v1/application-restore-plans
/v1/application-validation-runs
~~~

The WebUI must show:

- Install payload, saves, configuration, mods, caches, secrets, entitlement, activation, and runtimes as separate components.
- Exact, source-bound, unresolved, stale, blocked, and unprotected state per component.
- Why every asset belongs to a collection and where membership conflicts exist.
- Which components require network access, credentials, execution, or manual action.
- Capture consistency, source freshness, last clean cold acquisition, and last isolated validation.
- Explicit warnings that a store listing, cloud-sync status, launcher repair option, or successful launch is not complete recovery proof.

## 17. Failure and blocked states

Required stable reason codes include:

- `BLOCKED_COLLECTION_INCOMPLETE`
- `BLOCKED_COLLECTION_MEMBERSHIP_CONFLICT`
- `BLOCKED_PLATFORM_PROFILE_UNSUPPORTED`
- `BLOCKED_CAPTURE_DRIVER_UNAVAILABLE`
- `BLOCKED_CAPTURE_QUIESCE_FAILED`
- `BLOCKED_CAPTURE_RESUME_FAILED`
- `BLOCKED_SOURCE_BINDING_INCOMPLETE`
- `BLOCKED_SOURCE_COLD_TEST_STALE`
- `BLOCKED_ENTITLEMENT_OR_RIGHTS`
- `BLOCKED_CREDENTIAL_UNAVAILABLE`
- `BLOCKED_DEVICE_BOUND_ACTIVATION`
- `BLOCKED_REQUIRED_RUNTIME_UNAVAILABLE`
- `BLOCKED_REQUIRED_MOD_OR_DLC_MISSING`
- `BLOCKED_CLOUD_STATE_CONFLICT`
- `BLOCKED_DYNAMIC_EXECUTION_NOT_AUTHORIZED`
- `BLOCKED_DYNAMIC_SANDBOX_UNAVAILABLE`
- `FAILED_STATIC_APPLICATION_VALIDATION`
- `FAILED_DYNAMIC_APPLICATION_VALIDATION`
- `FAILED_RESTORED_STATE_LOAD`

None of these states may be summarized as successful collection recovery.

## 18. Delivery phases

### Phase 0 and Phase 1 MVP

- `RW-MVP-1` may include an optional read-only Steam resolver for a selected qualified platform profile; no operating system is required by the product-wide contract.
- Its output is advisory collection evidence that may enrich the baseline catalog and search index without changing recovery authority.
- It performs no network contact, capture hook, source-only omission, installation, application-aware restoration, or execution.
- Resolver failure preserves the generic exact route, exact fallback, and baseline search over platform-neutral fields and does not fail the MVP release.

### Phase 2

- Qualify one named read-only application or game collection-resolution `PlatformProfile` for one operating-system and ecosystem combination.
- Add one store or publisher `RetrieverDriver` profile with immutable source bindings, quarantine, and cold acquisition.
- Add WebUI review for components, membership conflicts, source state, rights, privacy, and exact-versus-reacquirable treatment.
- Keep dynamic execution disabled and require exact fallback through approval and grace.

### Phase 3

- Qualify one application-consistent `CaptureDriver` profile, core-owned application restore profile, and static `Processor.VALIDATE` capabilities for supported platform profiles.
- Add isolated opt-in dynamic validation for narrowly supported applications or games.
- Expand to additional profiles, accounts, launchers, mod managers, compatibility layers, and cross-volume collections only after independent conformance.
- Permit source-only treatment only for explicitly qualified components whose cold tests, RTO, rights, fallback, and approval gates pass.

### Phase 4 and later

- Add cross-platform state migration, disposable VM validation labs, multi-host application collections, and more complex transactionally consistent capture only through new explicit profiles and release gates.
- Do not infer compatibility or functional equivalence across application versions, platforms, emulators, or compatibility layers.

## 19. Conformance and cold-restore tests

Every shipped platform profile must test:

1. Install payload separated from saves, configuration, mods, caches, secrets, entitlement, and activation.
2. Multiple user profiles, side-by-side versions, branches, locales, architectures, DLC sets, and mod configurations.
3. Assets shared across collections and conflicting resolver evidence.
4. Missing install roots, offline volumes, permission failures, partial scans, profile upgrades, and application mutation during resolution.
5. Quiesce success, timeout, cancellation, worker crash, hook failure, and guaranteed resume or critical blocked state.
6. Clean cold acquisition with no payload cache, followed by independent artifact, signature, component, and malware validation.
7. Provider drift, removed builds, expired entitlement, wrong region, missing runtime, unavailable credentials, and network denial.
8. Saves and configuration from a newer version restored beside an older or changed install payload.
9. Missing mods, incorrect load order, absent DLC, incompatible runtime, and cloud-state conflicts.
10. Device-bound activation or secret material that cannot be ported.
11. Restore with cloud synchronization, auto-update, telemetry, post-install hooks, and executable launch disabled.
12. Attempted execution without approval and attempted sandbox, filesystem, credential, child-process, or network escape.
13. Static validation without execution and separately approved isolated dynamic validation.
14. A complete clean-environment restore that reports the requested and achieved outcome for every required component.
15. Resolver, source, capture, restore, or validator removal while exact filesystem recovery remains available.

Any silent omission of user state, unauthorized execution or network contact, false collection-complete result, unreported missing component, or automatic weakening of exact protection is a release blocker.
