# CLI and MCP Contract

## 1. Purpose

RestoreWeave exposes one stable automation contract for a self-hosted, content-first managed data layer. People, scripts, NAS integrations, WebUIs, and external AI harnesses use the same typed operations to inspect sources, plan storage, publish snapshots, search derived indexes, save dynamic views, freeze and materialize export manifests, read content, verify recovery, and restore data.

The content/view/export defaults in [Content Store, Views, and Export Requirements](content-store-views-and-exports.md) are authoritative. The command ABI has no mount/unmount family; the default user-facing output is an explicit frozen export manifest.

The CLI is the primary human and scripting surface. The initial MCP adapter is local and read-only. A REST service or WebUI may be added as another adapter over the same operations; it does not create a second domain model.

The core does not contain an agent loop, prompt memory, model runtime, or generic workflow harness. External intelligence calls typed operations. Optional processors, index providers, and query providers are selected through separately versioned extension contracts. An `IndexProvider` builds or updates a named, versioned, rebuildable index generation. Each `QueryProvider` invocation queries exactly one explicitly named `IndexGenerationRef`; compatibility is validated before invocation. A host-owned broker may fuse several separately generation-pinned typed provider results. One implementation may implement both extension contracts.

This contract is read with:

- [Core Kernel and Interface Requirements](core-kernel-and-interface.md).
- [System Architecture](system-architecture.md).
- [Driver and Processor Interfaces](driver-and-processor-interfaces.md).
- [Core Protocol and Reference Userland](../technical/core-protocol-and-reference-userland.md).
- [Namespace and Content Access](../technical/namespace-and-content-access.md).

The key words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are normative.

## 2. Product boundary

RestoreWeave owns:

- Typed source, capture, plan, job, snapshot, namespace, file-version, content, representation, placement, and verification identities.
- Deterministic planning and explicit recovery-policy decisions.
- Durable operation state, idempotency, cancellation, fencing, and reconciliation.
- Portable publication commits and clean-machine recovery.
- Authorization and selection of the representation used for reads and restores.
- Incremental update, logical deletion, retention, and garbage-collection semantics.

Adapters MAY:

- Render human workflows.
- Submit bounded commands.
- Read ordered events.
- Browse and search authorized subjects.
- Open bounded content handles.
- Display processor, repository, index, and query-provider capabilities.

Adapters MUST NOT:

- Publish a snapshot by writing a database row.
- Treat a model, processor, or query score as human authority.
- Invoke arbitrary plugins, SQL, shell commands, prompts, or URLs through the core.
- Expose unrestricted host paths, repository credentials, signing keys, or backend-private object IDs.
- Substitute an approximate representation for an exact read.

Exact ingest, verification, and restore MUST remain useful with no MCP client, LLM, WebUI, or external search provider installed. The reference discovery experience includes its bundled local embedding worker and zvec generation; their failure is a visible degraded state.

## 3. Interface principles

1. **One semantic contract.** CLI JSON, CLI JSON Lines, MCP, and future REST or UI adapters map to the same operation identifiers, references, statuses, reasons, and event sequence.
2. **Plan before mutation.** Repository writes, restore-destination writes, retention changes, migration, and physical GC require an immutable plan and expected digest.
3. **Exact by default.** Unknown, unsupported, conflicting, or optional-processor-failed readable data uses exact preservation. Similarity never proves identity.
4. **Opaque references.** Machine clients pass typed references rather than database IDs or reconstructed paths.
5. **Bounded bytes.** Large content moves through range handles, files, or streams rather than ordinary JSON or MCP messages.
6. **No implicit authority.** TTY presence, chat confirmation, a model response, and an MCP call do not create approval.
7. **Read-only automation first.** Initial MCP tools inspect committed and local durable state but cannot start capture, apply plans, restore, delete, migrate, or garbage-collect.
8. **Search is advisory.** A query provider returns subject candidates. The core resolves authorization and content access.
9. **Machine output is deterministic.** Clients never parse human prose or terminal layout.
10. **Adapters are replaceable.** Removing REST, WebUI, MCP, or a query provider cannot change recovery meaning.

## 4. Canonical transport-neutral contract

### 4.1 Command envelope

Every operation uses this semantic envelope:

~~~json
{
  "schema": "org.restoreweave.command.v1",
  "request_id": "01K2AB3CD4EF5GH6JK7M8NP9QR",
  "operation": "snapshot.list",
  "workspace_ref": "workspace:default",
  "idempotency_key": null,
  "input": {}
}
~~~

Rules:

- `schema` identifies the public major version.
- `request_id` is caller-generated when practical and is returned unchanged.
- `operation` is a registered identifier from this contract.
- `workspace_ref` selects an authorized self-hosted workspace.
- `idempotency_key` is REQUIRED for commands that create durable state or external effects.
- `input` is operation-specific and strictly decoded.
- Unknown critical fields or unsupported major versions fail closed.
- Future optional extension data belongs under a namespaced `extensions` member.

The adapter authenticates transport credentials and forwards immutable claims. The core derives the effective actor, workspace, client identity, and grants. A request body cannot self-assert authority.

### 4.2 Result envelope

~~~json
{
  "schema": "org.restoreweave.result.v1",
  "request_id": "01K2AB3CD4EF5GH6JK7M8NP9QR",
  "operation": "snapshot.list",
  "status": "SUCCEEDED",
  "started_at": "2026-08-12T03:00:00Z",
  "finished_at": "2026-08-12T03:00:00Z",
  "job_ref": null,
  "resource_refs": {},
  "reasons": [],
  "artifacts": [],
  "data": {}
}
~~~

`ResultStatus` is the closed set:

| Status | Meaning |
| --- | --- |
| `ACCEPTED` | Durable work was accepted and has not reached a terminal result. |
| `SUCCEEDED` | The requested contract completed and all required checks passed. |
| `DEGRADED` | Work completed with a declared weaker or partial outcome. |
| `BLOCKED` | Safety, policy, approval, capability, or state prevented unsafe work. |
| `FAILED` | An operational or integrity failure occurred. |
| `CANCELLED` | Cancellation reached a safe terminal state. |
| `UNKNOWN_EXTERNAL_OUTCOME` | Reconciliation could not prove whether a named external effect committed. |

Rules:

- `job_ref` is present for durable asynchronous work.
- `resource_refs` contains only stable typed references.
- `artifacts` describes intentionally emitted files or streams.
- `data` is defined by the named operation.
- Secrets, unrestricted host paths, and repository-private locators MUST NOT appear.
- Repository upload alone cannot produce a verified recovery claim.

### 4.3 Reason objects

Every non-success terminal result contains at least one reason:

~~~json
{
  "code": "PROCESSOR_UNAVAILABLE_EXACT_FALLBACK_USED",
  "class": "DEPENDENCY",
  "severity": "WARNING",
  "message": "The optional media classifier was unavailable, so the file remains protected exactly.",
  "retryable": true,
  "subject_ref": "subject:01K2AB7M9Q2R4ST6VW8XYZ1CDE",
  "resolution": {
    "action": "RETRY_OPTIONAL_PROCESSOR",
    "arguments": {}
  },
  "details": {}
}
~~~

Clients branch on `status`, `code`, `class`, and `retryable`, never on `message`.

`class` is one of:

- `INPUT`
- `AUTHORIZATION`
- `POLICY`
- `CAPABILITY`
- `DEPENDENCY`
- `CONFLICT`
- `INTEGRITY`
- `TRANSIENT`
- `RESOURCE_LIMIT`
- `CANCELLED`
- `INTERNAL`

Initial common reason codes include:

- `APPROVAL_REQUIRED`
- `CAPABILITY_NOT_GRANTED`
- `CAPTURE_CHANGED`
- `CAPTURE_UNAVAILABLE`
- `CONTENT_NOT_AVAILABLE`
- `DESTINATION_NOT_EMPTY`
- `DESTINATION_OUTSIDE_ALLOWED_ROOT`
- `GC_REACHABILITY_CONFLICT`
- `HANDLE_EXPIRED`
- `IDEMPOTENCY_CONFLICT`
- `INDEX_REVISION_STALE`
- `INTEGRITY_CHECK_FAILED`
- `INVALID_INPUT`
- `NO_EXACT_REPRESENTATION`
- `PLAN_DIGEST_MISMATCH`
- `PLAN_EXPIRED`
- `PLAN_NOT_EXECUTABLE`
- `PROCESSOR_RESULT_REJECTED`
- `PROCESSOR_UNAVAILABLE_EXACT_FALLBACK_USED`
- `PAGE_TOKEN_EXPIRED`
- `PAGE_TOKEN_SCOPE_MISMATCH`
- `QUERY_PROVIDER_UNAVAILABLE`
- `REPOSITORY_TARGET_CHANGED`
- `REPOSITORY_UNAVAILABLE`
- `REPRESENTATION_DEPENDENCY_MISSING`
- `REVIEW_DECISION_REQUIRED`
- `SNAPSHOT_NOT_FOUND`
- `SOURCE_OUTSIDE_ALLOWED_ROOT`
- `UNSUPPORTED_SOURCE_PROFILE`
- `VERIFICATION_INCOMPLETE`
- `EXTERNAL_OUTCOME_UNKNOWN`

Unknown reason codes are allowed within v1. Clients fall back to `class`, display the message, and never interpret an unknown reason as success.

### 4.4 Event envelope

Long-running operations emit ordered events:

~~~json
{
  "schema": "org.restoreweave.event.v1",
  "request_id": "01K2AB3CD4EF5GH6JK7M8NP9QR",
  "job_ref": "job:01K2AB8FG9HJ1KM2NP3QRST4VW",
  "sequence": "17",
  "time": "2026-08-12T03:04:00Z",
  "kind": "PROGRESS",
  "phase": "VERIFY",
  "subject_ref": "snapshot:01K2AB6CDE7FG8HJ9KM1NP2QRS",
  "data": {
    "completed_bytes": "1073741824",
    "total_bytes": "4294967296"
  }
}
~~~

Requirements:

- `sequence` is a monotonically increasing decimal string per job.
- Potentially large integer values are decimal strings.
- Times are UTC RFC 3339 values.
- Initial event kinds are `ACCEPTED`, `PHASE_STARTED`, `PROGRESS`, `WARNING`, `ARTIFACT_AVAILABLE`, `CHECKPOINT`, and `RESULT`.
- Exactly one terminal `RESULT` event contains the complete result envelope.
- Consumers ignore unknown event kinds while preserving sequence tracking.
- Progress is optional and sampled; correctness never depends on receiving every progress event.
- Estimates identify their capture, plan, processor, repository-capability, and provisional or final status.

### 4.5 Resource references and digests

Stable references use opaque typed strings, for example:

- `source:...`
- `capture:...`
- `plan:...`
- `job:...`
- `snapshot:...`
- `path:...`
- `file-version:...`
- `content:...`
- `representation:...`
- `placement:...`
- `subject:...`
- `repository:...`
- `processor-profile:...`
- `index:...`
- `query-provider:...`

Cryptographic digests use algorithm-qualified lowercase forms such as `sha256:<hex>`.

Display paths are for people. Machine clients SHOULD pass `path_ref`. A CLI convenience form such as `<snapshot-ref>:<display-path>` is resolved component-by-component within the authenticated snapshot and is never interpreted as a repository locator.

## 5. Stable operation families

### 5.1 Baseline operation set

| Operation | Side effect | Purpose |
| --- | --- | --- |
| `config.get` | None | Read the validated persisted operator profile, its path, persisted digest, running digest, and restart state without exposing secret values. |
| `config.update` | Atomic configuration-file write | Replace the persisted profile after full schema validation and an `expected_config_digest` check; live services are not hot-swapped and the result reports whether restart is required. |
| `plan.ingest` | Local capture and immutable plan state | Inspect a qualified source, identify content, select processors and representations, estimate incremental storage, and produce a reviewable ingest plan without repository publication. |
| `plan.revise` | Local plan and decision records | Create an immutable successor plan from explicit candidate decisions. |
| `plan.abandon` | Local capture-hold release | Abandon one unapplied plan and release its retained capture when safe. |
| `plan.restore` | Local immutable plan state | Preflight exact restore from a committed snapshot without destination writes. |
| `plan.get` | None | Read a plan, decisions, estimates, dependencies, and validity. |
| `plan.apply` | Yes | Apply one immutable ingest or restore plan digest. |
| `status.get` | Bounded reconciliation | Read system, source, repository, job, snapshot, index, or provider health. |
| `job.events` | None | Read a bounded ordered event page. |
| `job.cancel` | Bounded control effect | Request safe cancellation of durable work. |
| `snapshot.list` | None | List authenticated committed snapshots. |
| `snapshot.diff` | None | Compare two committed namespace generations. |
| `snapshot.verify` | Verification reads and evidence writes | Verify a declared snapshot scope and append evidence. |
| `recovery.export` | Bounded artifact creation | Export a portable recovery closure or independently retainable recovery reference. |
| `recovery.anchor.export` | Bounded artifact creation | Export the public Ed25519 trust anchor required to authenticate signed publication commits; never export private signing material. |
| `content.list` | None | List the latest workspace content as a flat library projection with path provenance, entry type, exact content identity, and logical size; directory traversal remains a separate `namespace.list` view. |
| `namespace.list` | None | List one directory in a committed snapshot. |
| `namespace.resolve` | None | Resolve path components and return an opaque path reference. |
| `namespace.stat` | None | Read recorded entry and file-version metadata. |
| `namespace.readlink` | None | Read the recorded raw symbolic-link target. |
| `representation.list` | None | List representations and health for one authorized subject. |
| `content.open` | None | Open a bounded representation read session. |
| `content.read` | None | Read a bounded range from an open session. |
| `content.close` | Bounded local release | Close a session before expiry. |
| `annotation.list` | None | List durable tag and note records for one authorized subject or scope. |
| `annotation.upsert` | Durable annotation write | Create or revise a tag or note with optimistic revision checks. |
| `annotation.delete` | Durable tombstone write | Tombstone one tag or note revision without mutating index state directly. |
| `annotation.export` | Bounded artifact creation | Export an authenticated portable annotation bundle. |
| `annotation.import` | Durable annotation write | Import a portable bundle after subject and revision validation. |
| `description.list` | None | List a bounded page of description revision summaries without returning full bodies. |
| `description.get` | None | Read one full description revision and its ordered semantic segments. |
| `description.create` | Durable description write | Create an immutable user, imported, extracted, or model-labeled description revision and its source-aligned segments. |
| `view.save` | Durable catalog mutation | Create a revisioned dynamic query and presentation policy. |
| `view.get` | None | Read one saved view revision without evaluating membership. |
| `view.evaluate` | None | Evaluate one saved view against explicit generation references. |
| `export.plan` | None | Freeze subjects, representations, names, target profile, and expected effects into an immutable `ExportManifest`. |
| `export.apply` | Destination mutation | Materialize one exact export-manifest digest to an explicit destination. |
| `export.verify` | Bounded read | Verify materialized output against its manifest and selected representations. |
| `capability.list` | None | List configured capture, processor, repository, index, and query capabilities. |
| `search.query` | None | Query one exact named index generation and return authorized subject candidates. |

`snapshot.verify` writes evidence but does not mutate the snapshot. `content.close` and bounded reconciliation are operational cleanup, not dataset mutation.

Adding a new processor, index, query provider, repository driver, capture driver, REST adapter, or UI MUST NOT change the meanings above.

### 5.2 Later lifecycle operations

The ABI reserves later plan families:

- `plan.retention`
- `plan.migration`
- `plan.representation-retirement`
- `plan.gc`

They follow the same immutable plan and `plan.apply` rule. They are not part of the initial MCP tool set. Physical GC and retirement of the last accepted exact representation require separately qualified authority, reachability, fencing, reconciliation, and post-operation verification.

## 6. Ingest planning

`plan.ingest` describes managed ingest into the self-hosted archive and search layer.

Example input:

~~~json
{
  "source": {
    "source_ref": "source:nas-media",
    "inline_root": null,
    "capture_profile_ref": "capture-profile:filesystem-consistent-v1"
  },
  "base_snapshot_ref": "snapshot:01K29ZZZZZZZZZZZZZZZZZZZZZ",
  "targets": [
    {
      "repository_ref": "repository:primary"
    }
  ],
  "options": {
    "processor_profile_refs": [
      "processor-profile:default-identification"
    ],
    "analysis_depth": "PROGRESSIVE",
    "network": "TARGETS_AND_GRANTED_PROCESSORS",
    "budget": {
      "max_bytes_read": null,
      "max_duration_ms": null,
      "max_compute_cost": null
    }
  },
  "proposal_refs": []
}
~~~

`source_ref` selects a configured NAS, filesystem, share, or connector source. `inline_root` is an authorized local convenience for supported deployments. Exactly one is supplied. Platform-specific paths remain adapter inputs and do not become portable source identity.

### 6.1 Protection decisions and external locators

Every ingest plan carries a default protection mode and MAY override it per proposed subject:

- `STORE_EXACT`
- `STORE_EXACT_WITH_EXTERNAL_FALLBACK`
- `LINK_ONLY`
- `METADATA_ONLY`

`LINK_ONLY` for readable bytes requires a distinct confirmation in the reviewed plan. Confirmation cannot be inferred from configuration, locator presence, a processor failure, or repository unavailability. A rejected or missing confirmation produces `BLOCKED`; it MUST NOT silently fall back to metadata-only.

Each external binding contains one or more ordered locator inputs:

~~~json
{
  "path": "relative/path.ext",
  "kind": "HTTPS",
  "locator": "https://example.invalid/path.ext",
  "display_locator": "example release mirror",
  "credential_ref": "keychain://restoreweave/example",
  "rights_evidence_ref": "entitlement:example"
}
~~~

`path` scopes a locator to one captured regular file. A target ABI MAY instead use a stable proposed-subject reference after observation. Multiple locator records for the same subject are alternatives in priority order. Credentials are references and MUST NOT be embedded in `locator` or `display_locator`. The applied record adds expected content identity and logical length when source bytes were readable. Locator presence alone remains `UNVALIDATED` and cannot produce `RESTORE_VERIFIED`.

The current development harness accepts a tree default plus explicit per-file overrides:

~~~json
{
  "root": "/authorized/local/tree",
  "protection_mode": "LINK_ONLY",
  "file_protection": {
    "keep-local.bin": "STORE_EXACT",
    "facts-only.txt": "METADATA_ONLY"
  },
  "confirm_link_only": true,
  "external_locators": [
    {
      "path": "relative/path.ext",
      "locator": "https://example.invalid/path.ext"
    }
  ]
}
~~~

The matching CLI is `rw ingest <root> --protection LINK_ONLY --file-protection 'keep-local.bin=STORE_EXACT' --file-protection 'facts-only.txt=METADATA_ONLY' --confirm-link-only --locator 'relative/path.ext=https://example.invalid/path.ext'`. Repeating `--locator` records alternatives. An unscoped locator is accepted only when the captured tree contains one regular file. The current harness performs read-only capture inspection and records a `READY` immutable plan with estimates, config digest, source basis, and per-file planned outcome/reason/identity; it does not publish repository data. The reviewed plan is executed separately with `rw plan apply <plan-id> --workspace <workspace-id> --digest <plan-digest>` after the plan, protection, configuration, and source checks pass.

Planning MAY retain a qualified capture and invoke explicitly selected read-only processors. It MUST NOT publish repository data, mutate source content, delete a source, or contact a network destination outside the selected targets and granted processor profiles.

The plan result includes:

- Plan reference, digest, intent, creation time, expiry, and validity conditions.
- Source, capture, capture-profile, and filesystem-semantics references.
- Base snapshot and incremental comparison basis.
- Total observed entries and bytes, plus explicit failures.
- Suffix, magic-byte, parser, learned-classifier, and conflict evidence summarized by class.
- Unique, duplicate, unknown, unsupported, unreadable, changing, and exact-fallback bytes.
- Selected processor graph, versioned parameters, and fallback behavior.
- Exact and derived representation decisions.
- Expected repository reuse, new stored bytes, transfer, compute, and duration ranges.
- Required decoders, credentials, models, and long-term dependencies.
- Proposed index-feed effects and derivatives without making the index authoritative.
- Every explicit exclusion, unresolved decision, risk, and required human action.
- Exact file-version and namespace coverage expected at publication.
- Target capability revisions and failure-domain assessment.

Every plan and snapshot result exposes one versioned `StorageAccounting` waterfall. A value is marked `ESTIMATED`, `OBSERVED`, or `UNAVAILABLE`, and the record names its source scope, repository scope, retention window, durability basis, and comparator. It contains at least:

- Logical source bytes.
- Unique exact bytes after full-file identity.
- Informational duplicate bytes, which are not added to repository chunk savings.
- Bytes reused from prior repository state.
- New exact bytes presented to the repository.
- Compression effect reported by the repository when available.
- Pack, encryption, parity, and repository metadata overhead.
- Actual repository growth when observable.
- RestoreWeave catalog, annotation, and index growth.
- Temporary peak working-space growth.
- Source bytes still retained.
- Whole-system net footprint change.
- Potentially reclaimable source capacity that has not been released.
- Released source capacity.

For `RW-MVP-1`, released source capacity is always zero. Archive-only savings and whole-system savings are different scopes, and overlapping duplicate, chunk-reuse, and compression effects MUST NOT be summed as independent savings.

The initial exact profile MUST NOT omit bytes merely because they appear downloadable, reproducible, duplicated, low-value, generated, or perceptually replaceable. Such evidence may create a review candidate. Only an explicit durable decision or already-published policy may weaken the ingest contract.

Optional processor failure on readable bytes produces `PROCESSOR_UNAVAILABLE_EXACT_FALLBACK_USED`. A profile-specific processor requirement MAY block only that processing branch, derived representation, or stronger profile claim. It MUST NOT block capture, inventory, host-owned exact hashing, exact placement eligibility, required exact-lane verification, or exact publication of readable bytes.

### 6.1 Identification evidence

For every planned content class, the result distinguishes:

- Extension or suffix hint.
- Magic-byte or container-signature evidence.
- Strict parser evidence.
- Optional learned or AI evidence.
- Confidence and inspected coverage.
- Conflicts and selected deterministic policy outcome.

No single classifier field becomes a deletion or fidelity decision.

### 6.2 Incremental planning

When `base_snapshot_ref` is present, the plan reports:

- Added, removed, moved, content-changed, metadata-changed, and uncertain entries.
- Reused file versions, content, representations, and placements.
- Processor derivatives that remain valid because all inputs and producer revisions match.
- Reprocessing required by algorithm, policy, or model upgrades.
- Estimated logical bytes versus new physical bytes.

An uncertain observation is not reported as a confirmed source deletion.

## 7. Plan revision, abandonment, and application

### 7.1 Plan revision

~~~json
{
  "base_plan_ref": "plan:01K2ABCDEF1GH2JK3M4NP5QRST",
  "base_plan_digest": "sha256:7c9f...",
  "candidate_decisions": [
    {
      "candidate_id": "candidate:generated-cache:01K2...",
      "decision": "EXPLICITLY_UNPROTECTED",
      "decision_authority_ref": "decision:local-human:01K2...",
      "reason": "The operator accepted regeneration risk for this cache."
    }
  ]
}
~~~

`plan.revise` never edits the base plan. For the supported ingest protection decisions, it creates a successor with a new digest, records decision authority, re-inspects the source, and recomputes the per-entry protection outcomes, protection digest, and storage estimates that depend on those decisions. Unsupported decision types MUST be rejected rather than represented as if their consequences had been recomputed. The successor acquires any required capture hold for its own lineage.

The current narrow decision-file schema is:

~~~json
[
  {
    "path": "relative/unreadable.bin",
    "mode": "METADATA_ONLY",
    "reason": "The operator accepts that only stable namespace metadata will be retained."
  }
]
~~~

For a path retained as `BLOCKED` or `UNAVAILABLE` in the base plan, this decision is an explicit operator-resolution request, not an automatic fallback. A successor becomes executable only if a fresh rooted-FD scan sees a regular file with identical before/after metadata, an included checked boundary, and no lstat, boundary, post-stat, or stability failure. Unstable entries, changed paths or handles, directories, symlinks, special files, path-string captures, cancelled scans, failed scans, and any unapproved entry remain blocked. A successful resolution publishes an `EXPLICITLY_UNPROTECTED` namespace fact with no `ContentID`, file version, representation, or recovery reference; it preserves `INCOMPLETE` scan authority and blocks exact restore.

### 7.2 Plan abandonment

`plan.abandon` marks only the named unapplied plan abandoned. It releases the plan's capture hold when no other plan, job, read, or processor invocation depends on that capture. It does not delete source data or published snapshots.

### 7.3 Plan application

`plan.apply` input includes:

~~~json
{
  "plan_ref": "plan:01K2ABCDEF1GH2JK3M4NP5QRST",
  "plan_digest": "sha256:7c9f...",
  "expected_target_revisions": {
    "repository:primary": "revision:42"
  }
}
~~~

Apply is the sole stable public mutation for protection and restore plans. It revalidates the plan digest, capture, policy, target capabilities, dependencies, authorization, and resource budget before the first external effect.

Protection apply performs the finite sequence:

~~~text
revalidate
-> ingest selected exact and approved derived representations
-> reconcile payload receipts
-> validate namespace and representation coverage
-> pass authenticated-metadata verification gate
-> prepare and reconcile portable closure
-> sign, store, and reconcile publication commit
-> project committed snapshot locally
-> update replayable index feeds
-> perform configured post-publication verification
~~~

The reconciled `PUBLICATION_COMMIT` placement is the portable logical commit point. A local pointer, index update, or successful upload cannot substitute for it.

For an ingest plan, the apply result MAY include the receipt-bound diagnostic
fields `savings_measured`, `new_physical_bytes`, and
`compression_saved_bytes`. These values cover newly placed exact payload
objects from that apply only. They do not include portable records, catalog,
indexes, models, temporary space, or whole-repository growth. If placement
crossed an unknown-outcome boundary or its receipt cannot support the
measurement, `savings_measured` is false and clients MUST display the values
as unavailable rather than infer zero savings from repository state.

Restore apply stages, materializes, verifies, and finalizes only the destination declared by the immutable restore plan.

## 8. Restore planning

`plan.restore` accepts either a locally known committed snapshot or a clean-recovery selector.

Example local input:

~~~json
{
  "snapshot_ref": "snapshot:01K2AB6CDE7FG8HJ9KM1NP2QRS",
  "selection": {
    "path_refs": [
      "path:01K2AB9JKLM2NP3QRST4VW5XYZ"
    ]
  },
  "destination": {
    "path": "/srv/restore-staging"
  },
  "contract": {
    "content": "EXACT",
    "filesystem_metadata": "REQUIRE_DECLARED_PROFILE"
  }
}
~~~

Example clean-recovery selector:

~~~json
{
  "discovery": {
    "repository_ref": "repository:recovery-target",
    "publication_commit_ref": null,
    "latest": true,
    "trust_anchor_ref": "trust-anchor:offline-primary"
  },
  "selection": {
    "path_refs": []
  },
  "destination": {
    "path": "/srv/restore-staging"
  },
  "contract": {
    "content": "EXACT",
    "filesystem_metadata": "REQUIRE_DECLARED_PROFILE"
  }
}
~~~

Planning performs no destination write. It reports:

- Selected snapshot, namespace root, paths, file versions, and exact representations.
- Required repositories, decoders, credentials, keys, and trust anchors as opaque references.
- Expected bytes, filesystem objects, conflicts, and destination capacity.
- Source-to-destination metadata fidelity.
- Unsupported special files, ACLs, attributes, sparse regions, links, or name semantics.
- Verification work required before finalization.
- Whether the plan is executable.

Clean recovery trusts only a valid signed publication commit verified against independently supplied trust material. Repository companion metadata cannot establish trust in its own signing key.

## 9. Read-only data operations

### 9.1 Snapshot listing and differences

`snapshot.list` returns committed snapshots with generation, sources, logical bytes, estimated physical bytes where known, verification level, placement health, and index revision summaries.

`snapshot.diff` returns added, removed, moved, content-changed, metadata-changed, type-changed, and uncertain entries between two authenticated namespace generations. Results use `path_ref` and `subject_ref` values and are paginated.

### 9.2 Namespace operations

`namespace.list` input includes snapshot or parent `path_ref`, page token, limit, and optional stable sort mode.

The page token is opaque and authenticated. It binds the interface version, exact snapshot and namespace-root digest, parent `path_ref`, sort and filter shape, principal and authorization revision, and expiry. It cannot be reused for another directory, snapshot, principal, authorization state, sort, or operation. Following one continuation chain returns every authorized child exactly once without duplication or omission. Retrying an identical request with the same token is idempotent; an expired or mismatched token fails with `PAGE_TOKEN_EXPIRED` or `PAGE_TOKEN_SCOPE_MISMATCH` rather than restarting or changing scope silently.

`namespace.resolve` accepts a snapshot and encoded path components. It returns an opaque `path_ref`. It does not follow symbolic links by default.

`namespace.stat` returns:

- Entry class and safe display name.
- File-version, content, and default representation references where authorized.
- Logical size and recorded metadata.
- Hard-link, sparse, symbolic-link, ACL, attribute, and named-stream summaries.
- Protection, placement, and verification health.
- Exact-fallback, explicit exclusion, or failure state.

`namespace.readlink` returns the recorded target bytes in a bounded encoding and never resolves them against the host.

### 9.3 Representations

`representation.list` returns representations attached to one subject or file version. Each result identifies:

- Class and fidelity claim.
- Producer and parameter revision.
- Expected output identity or validator.
- Decoder and dependency health.
- Placement and verification state.
- Whether it is the authoritative exact default.

Representation listing does not grant content access.

### 9.4 Content sessions

`content.open` input includes snapshot, path or file-version reference, requested access contract, optional explicit representation, and byte budget. The default access contract is exact.

The result returns a principal-bound handle with:

- Handle reference and expiry.
- Snapshot, file-version, content, and selected representation references.
- Logical length.
- Allowed range and per-read limit.
- Current verification context.

`content.read` accepts a handle, offset, and bounded length. It returns a transport-appropriate byte artifact plus digest and range metadata. `content.close` is idempotent.

An exact open fails with `NO_EXACT_REPRESENTATION` or another typed reason if the exact path is unavailable. It does not choose a similar image, song, video, document, or generated result.

### 9.5 Durable tags and notes

`RW-MVP-1` supports whole-subject tags and one or more plain-text note records. An `AnnotationRecord/v1` contains:

- `annotation_ref`, immutable `subject_ref`, and annotation kind `TAG` or `NOTE`.
- Canonical tag value or note content digest and bounded content reference.
- Author or actor reference, visibility scope, and provenance.
- Monotonic revision, predecessor revision, creation time, and update time.
- Tombstone state and deletion provenance.

`annotation.upsert` and `annotation.delete` require an expected current revision. A mismatch returns a typed conflict and writes nothing. They update authoritative annotation records first; index refresh is a derived asynchronous effect.

`annotation.export` produces an authenticated bundle containing subject bindings, exact revisions, provenance, and tombstones without repository credentials. `annotation.import` verifies the bundle, resolves compatible subjects, rejects revision forks unless an explicit conflict policy is supplied, and never depends on a search index.

The minimal file-only `LinkGroup` is the MVP grouping primitive defined by the
content-store contract. It is planned for Phase 6 and is not implemented by
the current command ABI. A group has one current membership map from
group-relative paths to stable file `SubjectRef` values; adding, removing, or
renaming a member updates that current state atomically. It has no
user-visible version number, revision chain, predecessor, successor, or
membership history. When an exact point-in-time result is required,
`ExportManifest` freezes the selected file versions at export time. Richer
Collections, nested groups, roles, ratings, relationship graphs, typed
segment annotations, recovery-intent services, and machine-suggestion review
remain later profiles. A missing or unavailable member remains in the current
map and is shown as unavailable; it is never silently dropped. An empty group
continues to exist until the user explicitly deletes the group. Deleting a
group removes only its membership and group subject, never the member files or
their repository objects, and never authorizes garbage collection.

### 9.6 Durable descriptions

Descriptions are separate from short `NOTE` annotations. `description.create` accepts one subject, kind (`USER`, `IMPORTED`, `EXTRACTED`, `AI_SUMMARY`, or `AI_ANALYSIS`), UTF-8 body, language, title, source and producer references, confidence/coverage, visibility, acceptance state, optional predecessor, and provenance metadata. A successor is a new immutable record; it never overwrites its predecessor, and one predecessor cannot silently fork into two successors.

The current reference command accepts at most 16 MiB of UTF-8 text and splits it at UTF-8-safe sentence or whitespace boundaries into ordered segments of at most 1024 source bytes where practical. Every segment retains a digest and `[start_byte,end_byte)` source span. `description.list` defaults to 100 summaries and accepts a limit from 1 to 1000; summaries exclude body and segment text. `description.get` returns one full revision and its segments.

These operations establish the catalog and command shape only. A description is not fully portable until its body, revisions, segments, provenance, config/profile digests, and authenticated bundle or recovery-closure reference survive loss of the operational SQLite catalog. Vector generations remain disposable and never become the only copy of description text.

## 10. Search and query operations

`search.query` is a stable read-only projection operation using `DiscoveryQuery/v1`. The core remains recoverable when no provider is configured, but the `RW-MVP-1` reference distribution MUST configure bundled lexical, structured, and local semantic `IndexProvider` and `QueryProvider` implementations, validate their compatibility, and activate generation-pinned default fusion.

Example input:

~~~json
{
  "query_provider_ref": "query-provider:lexical-default",
  "index_generation_ref": "index-generation:01K2BASELINE00000000000000",
  "stale_policy": "REJECT_STALE",
  "snapshot_scope": {
    "latest_committed": true,
    "snapshot_refs": []
  },
  "query": {
    "text": "quarterly experiment report",
    "filters": {
      "source_refs": [],
      "path_prefixes": ["/research/"],
      "entry_types": ["REGULAR_FILE"],
      "content_classes": ["document", "text"],
      "format_ids": [],
      "size_bytes": {"gte": 1, "lte": null},
      "modified_time": {"gte": null, "lt": null},
      "content_digests": [],
      "duplicate_group_refs": [],
      "tags_all": ["reviewed"],
      "tags_any": [],
      "note_text": null,
      "processing_states": ["CURRENT"],
      "representation_kinds": ["EXACT_RAW", "EXACT_REVERSIBLE"],
      "placement_states": ["PLACED"],
      "verification_levels": []
    },
    "projection": ["subject_ref", "path_ref", "file_version_ref", "content_ref", "matched_fields"],
    "sort": [{"field": "score", "direction": "DESC"}],
    "facets": ["content_class", "suffix", "processing_state"]
  },
  "page": {
    "limit": 50,
    "token": null
  }
}
~~~

The exact `index_generation_ref` is required at provider invocation. CLI porcelain may accept an active-generation selector, but the host-owned query broker resolves it to one immutable generation before provider dispatch. One `QueryProvider` invocation queries one generation. The broker may invoke several providers or generations and fuse their typed results, but continuation, authorization, and provenance remain explicit for every component. A presentation adapter authenticates transport credentials and forwards immutable claims; it does not choose the effective principal, workspace, or generation.

Supported baseline filters include source, snapshot, path prefix, entry type, content class, format, size range, recorded time range, exact digest, duplicate group, tag, note text, processing state, representation kind, placement state, and verification state. Queries may request bounded projections, stable sorting, facets, and explanations. The contract is database-like but does not expose arbitrary SQL, search-engine DSL, or index-private field names.

The result contains candidates with:

- Authorized `subject_ref` and optional `path_ref`.
- File-version, content, representation, placement, and verification references when requested and authorized.
- Exact IndexProvider generation, indexed-through snapshot or RRF revision, and QueryProvider profile.
- Score, score semantics, and bounded explanation.
- Matched fields, snippets, tag, note, fingerprint, embedding, or derivative references.
- Requested facets and projection fields.
- Staleness and completeness indicators.

Each snapshot's relationship to a generation is one of `PENDING`, `CURRENT`, `PARTIAL`, `STALE`, `FAILED`, or `UNAVAILABLE`, with indexed-through revision and coverage. `REJECT_STALE` fails rather than silently querying stale data; `ALLOW_STALE_WITH_STATUS` returns explicit status. A search page token is opaque and authenticated and binds the interface version, query digest, exact generation, provider profile, principal and authorization revision, projection, sort order, and expiry. It cannot cross a query, generation, provider, principal, authorization, projection, or sort change. Following one chain returns each authorized result at most once under the declared stable ordering; retrying an identical request is idempotent, and an invalid, expired, or out-of-scope token fails closed rather than restarting against a current generation.

The core query broker resolves and reauthorizes every candidate through the authoritative namespace and access policy before a presentation adapter receives it. Opening content requires `content.open`. Query results cannot publish, delete, restore, or select a weaker representation.

If no provider is configured, the operation returns `BLOCKED` with `QUERY_PROVIDER_UNAVAILABLE`. If the baseline provider is absent in `RW-MVP-1`, the installation is non-conforming. Exact browse and restore remain available.

`capability.list` reports configured processor, index, and query capabilities, versions, health, supported content classes, model spaces, and declared resource requirements. It does not expose invocation escape hatches.

## 11. Verification and recovery export

### 11.1 Snapshot verification

`snapshot.verify` declares one mode:

- `AUTHENTICATED_METADATA`
- `SAMPLED_CONTENT`
- `FULL_BYTES`
- `RESTORE_DRILL`
- `CLEAN_RECOVERY`

Input binds the snapshot, subject scope, quantitative sample policy where applicable, expected representation kind and recovery claim, and budget. The result records bytes and entries attempted, bytes and entries passed, decoder path, repository reads, failures, and accepted verification level.

A repository-native check, processor result, index lookup, or local cache hit is reported only at the level it actually proves.

### 11.2 Recovery export

`recovery.export` writes a new output artifact and never overwrites an existing file by default. The artifact contains or references by authenticated digest:

- The selected publication commit and RRF root.
- Required records, schemas, signatures, and reader compatibility data.
- Bound payload and prepared-closure receipts.
- Repository configuration without plaintext credentials.
- Clean-machine verification and restore instructions.
- An independently retainable recovery selector.

The export result reports artifact path or stream handle, digest, length, snapshot, and trust requirements. It never claims an additional failure domain unless the export is actually stored independently.

The operation accepts an optional subject scope and output form. Snapshot scope exports the portable closure bundle. Subject scope may export a deterministic signed `RecoveryToken` set: one token per selected `RecoveryReference`, including exact, reversible, and link-only references and their honest protection claims. A token export for `LINK_ONLY_UNPROTECTED` preserves that warning and MUST NOT imply that bytes are inside the token or currently reacquirable. A metadata-only subject returns its `EXPLICITLY_UNPROTECTED` record and no token.

The reference signed implementation writes a bundle containing the selected `PUBLICATION_COMMIT`, its `PREPARED_CLOSURE`, their authenticated digests, and the required trust-anchor key ID/digest. The bundle does not contain a private key and is not itself the trust root. A non-signed development profile may export the legacy portable snapshot form, but that profile does not satisfy the signed recovery contract.

### 11.3 Recovery trust-anchor export

`recovery.anchor.export` writes a new public trust-anchor artifact and refuses to overwrite an existing destination. The artifact contains the schema, signature domain, publication domain, writer identity, Ed25519 public key, key ID, and public-key digest. It MUST NOT contain a private key, credential, repository secret, or a claim that the anchor was independently retained. Verification and clean recovery require the operator to supply this anchor from a separately retained location.

## 12. Status, events, and cancellation

`status.get` may inspect:

- Controller health.
- Source and capture health.
- Repository capability and placement health.
- Job state.
- Snapshot publication and verification state.
- Processor, index-feed, and query-provider health.
- Retention and later GC planning state.

When an exact lane is attached, the current implementation returns the resolved repository path, `repository_profile`, `compression_profile`, health, and committed snapshot count. `doctor.check` repeats the active tuple and separately states that an in-tree candidate is not a selected release engine. Profile visibility is diagnostic evidence, not qualification.

It may perform bounded reconciliation and cleanup of already expired local handles or holds. It cannot publish, restore, retire, or garbage-collect data.

`job.events` accepts `job_ref`, `after_sequence`, and bounded `limit`. It returns ordered events, next sequence, and terminal indicator.

`job.cancel` requests cancellation. A driver effect already in progress is reconciled before the core reports a terminal outcome. Cancellation does not infer rollback of an effect the driver cannot prove was undone.

## 13. Human CLI porcelain

The reference CLI SHOULD provide:

~~~text
restoreweave doctor [<source-or-path>] [--to <repository>] [--credential <credential-ref>]
restoreweave capabilities

restoreweave plan <source-or-path> --to <repository> [--credential <credential-ref>] [--base <snapshot-ref>] [--processors <profile>] [--save-profile <name>]
restoreweave plan revise <base-plan-ref> --digest <base-plan-digest> [--decisions <json-file>]
restoreweave plan abandon <plan-ref>
restoreweave plan show <plan-ref>
restoreweave apply <plan-ref> --digest <plan-digest>
restoreweave profile run <name>

restoreweave status [<resource-ref>] [--events] [--after <sequence>] [--limit <count>]
restoreweave cancel <job-ref>
restoreweave snapshots [--source <source-ref>]
restoreweave diff <older-snapshot-ref> <newer-snapshot-ref>
restoreweave verify <snapshot-ref> [--mode authenticated-metadata|sampled-content|full-bytes|restore-drill|clean-recovery]

restoreweave browse [<snapshot-ref>[:<path>]]
restoreweave stat <snapshot-ref>:<path>
restoreweave representations <snapshot-ref>:<path>
restoreweave cat <snapshot-ref>:<path> [--representation <representation-ref>] [--to-file <new-file>]
restoreweave search <query> [--provider <query-provider-ref>] [--generation <index-generation-ref>] [--snapshot <snapshot-ref>] [--allow-stale]
restoreweave view save <name> --query <query>
restoreweave view show <view-ref>
restoreweave export plan (--view <view-ref> | --subject <subject-ref>...) --target <profile>
restoreweave export apply <export-manifest-ref> --destination <path-or-uri>
restoreweave export verify <export-manifest-ref> --destination <path-or-uri>
restoreweave tag list <subject-ref>
restoreweave tag add <subject-ref> <tag> [--expected-revision <revision>]
restoreweave tag remove <subject-ref> <tag> [--expected-revision <revision>]
restoreweave note list <subject-ref>
restoreweave note set <subject-ref> --from-file <file> [--expected-revision <revision>]
restoreweave note remove <subject-ref> [--expected-revision <revision>]
restoreweave annotations export [--subject <subject-ref>] --to-file <new-file>
restoreweave annotations import <file> [--conflict fail|keep-local|keep-imported]
restoreweave description list [<subject-ref>] --workspace <workspace-ref> [--limit <count>]
restoreweave description get <description-ref> --workspace <workspace-ref>
restoreweave description create <subject-ref> --workspace <workspace-ref> --kind <kind> (--body <text> | --body-file <path|->) [--predecessor <description-ref>]

restoreweave recovery export <snapshot-ref> --to-file <new-file>
restoreweave recovery anchor export <new-file>
restoreweave restore <snapshot-ref>:<path> <destination-path>
restoreweave restore --from <repository> --recovery-reference <file> --to <destination-path> --credential <credential-ref>
restoreweave restore --from <repository> --latest --trust <trust-anchor-ref> --to <destination-path>

restoreweave mcp serve --stdio
~~~

The concise `plan`, `apply`, `profile run`, `browse`, `search`, `view`, `export`, `tag`, `note`, `verify`, and `restore` commands are product porcelain over canonical operations. They do not add hidden semantics. Mounting is intentionally outside the command ABI. A saved ingest profile creates a fresh plan against current source and capability state; it never reapplies an old mutable plan.

Interactive `restore` porcelain first creates an immutable `plan.restore` result, displays its exact digest and consequences, and applies it only through `plan.apply` after the required confirmation and authority checks. Noninteractive and machine clients MUST call the two canonical operations explicitly; a convenience command cannot bypass the plan digest.

Every non-raw-content command accepts:

~~~text
--format human|json|jsonl
~~~

`--json` and `--jsonl` MAY be exact aliases. Human output MAY improve between releases. Machine output follows the versioned contract.

### 13.1 Human confirmation

Interactive confirmation is a presentation aid, not an authority record. A mutating CLI command still requires:

- An immutable executable plan.
- Exact plan digest.
- Required decision-authority records.
- Current capability and target revisions.
- Idempotency key.
- Applicable policy and grants.

Noninteractive mode fails closed when required authority is absent.

### 13.2 Saved profiles

The CLI MAY save named ingest profiles containing source reference, capture profile, repository targets, processor profiles, budgets, verification policy, and expected cadence. A profile run creates a new plan against a new qualified capture and current target capabilities. It does not replay an old mutable plan or bypass review requirements.

Scheduling remains external or reference-userland functionality. It calls the same plan and apply operations.

## 14. Stable CLI output

### 14.1 Human output

Human output emphasizes:

- What changed.
- Logical versus estimated new physical bytes.
- Unknown, unsupported, and exact-fallback scope.
- Processor and dependency risks.
- Exact and derived representation choices.
- Publication and verification health.
- Required decisions and safe next action.

Human output is not machine parseable.

### 14.2 JSON

`--format json` writes exactly one result envelope to standard output. Progress and logs go to standard error unless suppressed. No color, spinner, banner, or diagnostic prefix appears on standard output.

### 14.3 JSON Lines

`--format jsonl` writes zero or more event envelopes followed by exactly one terminal result event. Reconnection uses `job.events`; clients do not rely on a live process pipe as durable history.

### 14.4 Raw content

`cat` without `--to-file` writes only raw bytes to standard output. Metadata, warnings, and diagnostics go to standard error. A failing read MUST NOT append a JSON error to the byte stream.

`--to-file` uses create-new semantics by default, verifies the written range or full content according to the request, and reports the artifact through the ordinary result envelope.

## 15. Exit codes

The stable CLI exit classes are:

| Code | Meaning |
| --- | --- |
| `0` | `SUCCEEDED` |
| `2` | Invalid input or command usage |
| `3` | `BLOCKED` by policy, approval, capability, or conflict |
| `4` | `FAILED` operationally or by integrity check |
| `5` | `DEGRADED` |
| `6` | `CANCELLED` |
| `7` | `UNKNOWN_EXTERNAL_OUTCOME` |
| `8` | Authentication or authorization failure |

`ACCEPTED` returns `0` only when the caller explicitly requested asynchronous acceptance. A command that waits for completion returns the terminal result's exit class.

## 16. Initial local MCP adapter

### 16.1 Deployment

The initial adapter runs only as:

~~~text
restoreweave mcp serve --stdio
~~~

It opens no TCP listener and inherits the local process identity plus an explicit workspace configuration. It uses the same dispatcher, schemas, authorization, and reason codes as CLI JSON.

The server contains no model provider, prompt store, memory, agent loop, scheduler, or separate job system.

### 16.2 Initial read-only tool mapping

| MCP tool | Core operation |
| --- | --- |
| `restoreweave_status_get` | `status.get` |
| `restoreweave_plan_get` | `plan.get` |
| `restoreweave_job_events` | `job.events` |
| `restoreweave_snapshot_list` | `snapshot.list` |
| `restoreweave_snapshot_diff` | `snapshot.diff` |
| `restoreweave_namespace_list` | `namespace.list` |
| `restoreweave_namespace_resolve` | `namespace.resolve` |
| `restoreweave_namespace_stat` | `namespace.stat` |
| `restoreweave_namespace_readlink` | `namespace.readlink` |
| `restoreweave_representation_list` | `representation.list` |
| `restoreweave_content_open` | `content.open` |
| `restoreweave_content_read` | `content.read` |
| `restoreweave_content_close` | `content.close` |
| `restoreweave_annotation_list` | `annotation.list` |
| `restoreweave_capability_list` | `capability.list` |
| `restoreweave_search_query` | `search.query` |

The initial server does not expose:

- `plan.ingest`
- `plan.revise`
- `plan.abandon`
- `plan.restore`
- `plan.apply`
- `job.cancel`
- `snapshot.verify`
- `recovery.export`
- Any lifecycle, migration, retirement, or GC operation
- `annotation.upsert`, `annotation.delete`, `annotation.export`, or `annotation.import`

An external AI can inspect, compare, search, and read bounded content, then present a proposal to a human or call an independently authorized CLI workflow. It cannot mutate protected state through the initial MCP server.

### 16.3 MCP resources

The adapter MAY expose small schema-defined resources for:

- Product and capability summary.
- Snapshot summary.
- Plan summary.
- Namespace metadata.
- Representation metadata.
- Query-provider and index revision status.
- Job event pages.

Resources are bounded, authorized, and versioned. Large file content remains behind content handles.

### 16.4 Content safety

- `content.open` requires an authorized immutable file version.
- Handles bind principal, snapshot, representation, range budget, and expiry.
- `content.read` enforces per-call and session byte limits.
- The adapter MAY return a bounded binary resource or a host-owned temporary artifact reference.
- The adapter MUST NOT place unbounded base64 content in ordinary tool results.
- Tool descriptions label file content, extracted text, captions, and metadata as untrusted data rather than instructions.

### 16.5 Future mutation grants

A later experimental MCP mutation profile MAY expose selected plan creation or apply operations only after it defines:

- A separately enabled capability grant.
- Allowed workspace, source, repository, operation, and time scope.
- Immutable plan and digest requirements.
- Explicit human or published-policy authority.
- Idempotency, rate, byte, compute, and duration limits.
- Revocation and audit behavior.

Destructive lifecycle operations SHOULD remain unavailable to MCP until independently qualified. Enabling a mutation tool does not give an AI authority to weaken recovery fidelity.

## 17. Large-content handling

Control messages carry metadata and references, not bulk payloads.

Supported transfer patterns are:

- CLI raw standard output.
- CLI create-new output file.
- Principal-bound range handles.
- Host-owned temporary artifacts with expiry and digest.
- Future local descriptor or streaming transports negotiated by capability.

Every content artifact identifies:

- Snapshot, file version, content, and representation.
- Offset and length.
- Digest of returned bytes where practical.
- Whether the complete exact content identity was verified.
- Expiry and cleanup behavior.

Adapters enforce decompression, decoder-expansion, concurrent-read, and total-byte budgets.

## 18. Shared safety requirements

Every adapter MUST:

- Authenticate locally or through an explicitly configured identity provider.
- Derive authority outside the request body.
- Validate workspace and resource ownership.
- Reject paths outside configured source and destination roots.
- Resolve snapshot paths component-by-component.
- Redact credentials, tokens, signing keys, and sensitive repository locators.
- Enforce deadlines, cancellation, input size, output size, and concurrency limits.
- Record durable audit evidence for mutation and bounded access events according to policy.
- Preserve idempotency across retry and reconnect.
- Fail closed on unknown critical schema fields or unsupported major versions.
- Distinguish approximate search from exact content access.
- Never treat a query provider, processor, index, UI, or external harness as recovery authority.

## 19. REST and WebUI adapters

A future REST service or WebUI MAY provide remote administration, rich search, visual policy review, source and repository configuration, or live job monitoring. It MUST bind the same command, result, event, namespace, content, and query contracts.

Such an adapter MUST NOT:

- Write SQLite as a control shortcut.
- Create a second plan or job state machine.
- Rename statuses or reinterpret reason codes.
- Return repository-private storage paths as public identity.
- Bypass content handles for bulk downloads.
- Let a visual action skip immutable plan, digest, authority, or idempotency checks.

REST route shape, frontend framework, and UI layout are not stable core contracts. The typed operations are.

## 20. Compatibility policy

### 20.1 Stable surface

After v1 qualification, stability covers:

- Operation identifiers and semantic meanings.
- Command, result, reason, event, reference, digest, plan, and handle schemas.
- CLI machine-output flags and raw-content behavior.
- Exit-code classes.
- Initial read-only MCP tool names and mappings.
- `SnapshotTree`, `FileAccess`, exact-default, and portable-publication semantics.

### 20.2 Version rules

- A minor revision may add optional fields, operations, events, reason codes, or capabilities with defined unknown-value behavior.
- A breaking change requires a new major version.
- Clients ignore unknown optional fields and enum values where this contract defines a safe fallback.
- Unknown critical extensions fail closed.
- Experimental mutation tools and extension process protocols carry explicit `alpha` or `experimental` markers.
- Human CLI wording and layout may change without a major version.

### 20.3 Private implementation

The following are not compatibility surfaces:

- Go interfaces and packages.
- SQLite tables and migrations.
- Worker and daemon topology.
- Repository pack or chunk formats.
- Index schemas, vector spaces, tokenizer details, and ranking algorithms.
- Processor executable arguments and private IPC unless a separate external protocol freezes them.

## 21. Acceptance tests

### CM-AT-001: CLI and MCP equivalence

For every initial MCP tool, compare its result with CLI JSON for the same operation and principal. Status, references, reasons, pagination, and authorized bytes are equivalent.

### CM-AT-002: Read-only MCP

The initial MCP server exposes no plan creation, apply, restore, verify-evidence write, export, cancellation, retention, migration, retirement, deletion, or GC tool. Attempts to invoke unknown mutation tools fail without side effects.

### CM-AT-003: NAS-neutral planning

`plan.ingest` accepts a qualified Linux/NAS source profile and contains no field required only by an optional platform-specific capture implementation.

### CM-AT-004: Identification provenance

Plan output reports extension, magic-byte, strict-parser, and optional learned evidence separately, including inspected coverage and conflicts.

### CM-AT-005: Exact fallback

Crash or remove every optional processor. Readable data remains planned for exact ingest with typed warnings.

### CM-AT-006: Physical versus logical size

Plan and snapshot output provide the complete scoped accounting waterfall, distinguish overlapping effects, report actual repository and index growth where observable, keep released source capacity at zero for `RW-MVP-1`, and never claim precision the repository cannot prove.

### CM-AT-007: Packed namespace access

CLI browse and MCP namespace tools return the original paths for files stored in shared packs. No repository-private object locator appears.

### CM-AT-008: Exact representation selection

When exact and perceptual representations coexist, an unspecified `content.open` selects exact. Removing exact availability returns a typed failure rather than perceptual bytes.

### CM-AT-009: Search authorization

A provider returns stale, unauthorized, and nonexistent subject candidates. The core query broker resolves and filters them before any adapter response; no candidate grants content access.

### CM-AT-010: Index independence

Delete and rebuild every index. Browse, exact read, verification, restore, snapshot identity, and durable tag/note revisions remain unchanged.

### CM-AT-011: Plan and apply separation

Planning may retain a capture but performs no repository publication, source deletion, destination write, or GC. Apply rejects a changed plan digest or target capability revision before an unsafe effect.

### CM-AT-012: Idempotency and reconciliation

Retry an apply across timeout and process loss. The same idempotency key does not duplicate effects, and ambiguous driver outcomes reconcile or terminate as `UNKNOWN_EXTERNAL_OUTCOME`.

### CM-AT-013: Portable commit

A successful upload without a valid reconciled `PUBLICATION_COMMIT` is not listed as a published snapshot. A clean machine can recover from a valid commit without SQLite or an index.

### CM-AT-014: Incremental diff

`snapshot.diff` reports add, remove, move, content change, metadata change, and uncertainty while preserving both historical namespace generations.

### CM-AT-015: Large-content safety

Control responses remain bounded. Raw CLI output contains only bytes, MCP enforces range budgets, and a failing read never mixes structured error text into content.

### CM-AT-016: REST adapter consistency

A test REST adapter maps to the same dispatcher. Removing it leaves CLI and MCP behavior unchanged, and it cannot bypass plan digest or authorization rules.

### CM-AT-017: GC remains gated

No initial MCP tool can request physical GC. A later GC plan cannot apply without reachability proof, explicit authority, lease, fence, and reconciled repository receipt.

### CM-AT-018: Generation-pinned database-like query

A query with exact generation, typed filters, projection, sort, facets, and stale policy returns only authorized fields. Its continuation token fails after a generation, query, sort, or authorization change rather than crossing silently.

### CM-AT-019: Annotation durability

Create, revise, tombstone, export, and re-import tags and notes. Optimistic revision conflicts write nothing, index loss loses no annotation, and re-import preserves exact subject bindings and revisions.

### CM-AT-020: Export-manifest materialization

Freeze a saved view into an immutable `ExportManifest`, apply its exact digest to an empty destination, and verify every output against the selected representation. Re-evaluating the saved view may change membership; replaying the frozen manifest may not. Destination collisions, unsupported names or metadata, stale authorization, and unavailable representations fail with typed outcomes and do not silently rename or substitute content.

### CM-AT-021: MCP annotation boundary

Initial MCP can list authorized annotations but exposes no annotation mutation or export/import tool. CLI mutation and the read-only MCP view use the same records and authorization semantics.

### CM-AT-022: Scoped pagination

List a large immutable directory through CLI JSON and MCP with varied page sizes and repeated requests. One continuation chain returns every authorized entry exactly once, the same token retry returns the same page, and tokens fail closed after a principal, directory, snapshot, namespace-root, sort, filter, authorization-revision, or expiry change.

## 22. Explicit initial exclusions

The initial contract does not promise:

- Remote MCP transport.
- MCP mutation tools.
- A2A protocol support.
- An embedded AI harness or model service.
- An embedded vector database.
- Writable NAS semantics.
- Automatic source deletion.
- Automatic retirement of exact representations.
- Physical garbage collection through automation.
- P2P or magnet retrieval.
- Silent perceptual substitution.

These may be added through separately qualified adapters, processors, query providers, repository profiles, or lifecycle contracts without changing the stable exact-access core.
