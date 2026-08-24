# Optional REST and WebUI Adapter Requirements

## 1. Product position

RestoreWeave is a self-hosted content-aware managed data layer for NAS and heterogeneous storage, not a WebUI product. Its canonical behavior is the transport-neutral command, result, reason, plan, event, namespace, content, processor, index, and query contracts.

The initial qualified interaction surfaces are:

- A recovery-capable CLI with stable machine-readable output.
- An initial local `stdio`, read-only MCP adapter for scripts and external automation or AI harnesses.

A REST service and WebUI are valuable optional adapters for remote administration, multi-user access, job monitoring, search, and everyday NAS use. They MUST sit over the same typed operations and MUST NOT introduce a second source of truth, policy model, scheduler, job lifecycle, plan format, index authority, or recovery meaning.

The full remote profile remains deferred from the smallest core release. The
checkout includes a deliberately small loopback adapter and browser client as
an optional convenience surface. The underlying product MUST remain fully
operable without an HTTP server, browser, JavaScript runtime, or distributed
vector service. The reference local semantic profile is embedded in the core
distribution, but exact recovery does not depend on it.

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

## 2. Adapter architecture

~~~mermaid
flowchart TB
    CLI["CLI"] --> ABI["Typed Core Command ABI"]
    MCP["Local read-only MCP"] --> ABI
    REST["Optional REST adapter"] --> ABI
    UI["Optional WebUI"] --> REST
    Harness["External AI harness"] --> MCP

    ABI --> Core["RestoreWeave core"]
    Core --> Journal["Operation journal"]
    Core --> Records["Authoritative records"]
    Core --> Namespace["Namespace and content access"]
    Core --> Query["Index and query providers"]
~~~

The WebUI SHOULD use the REST adapter when remote browser access is needed. A local desktop or server-rendered UI MAY bind the command dispatcher directly, but it must preserve the same serialized operation semantics and conformance tests.

HTTP resources and status codes are transport concerns. The structured RestoreWeave result is the domain outcome. A `200`, successful upload, completed browser request, or rendered search result is not proof of durable placement, publication, verification, or recoverability.

### 2.1 Current local adapter

The current bounded implementation exposes:

- `GET /api/v1/healthz` for a transport-only liveness check;
- `POST /api/v1/command` with a strict `org.restoreweave.command.v1`
  envelope, returning the unchanged `org.restoreweave.result.v1` envelope;
- optional `Authorization: Bearer <token>` validation when the host supplies
  `RESTOREWEAVE_API_TOKEN` or `--api-token`.

The listener is disabled by default. `api.enabled` and `api.listen` in the
persisted TOML profile enable it; `--api-listen` is a one-shot override. The
adapter has no database, repository, filesystem, job, or recovery state of its
own. The React/Vite client under `web/` uses only these endpoints.

## 3. Canonical-operation mapping

Every adapter operation MUST map to one registered typed core operation. The REST adapter MUST NOT expose:

- Generic shell or process execution.
- Raw SQL or internal database tables.
- Arbitrary plugin invocation.
- Repository-private object or pack access.
- Unrestricted live-host filesystem reads.
- Plaintext credentials, signing keys, or internal secret-store APIs.
- A generic `execute`, `run_tool`, prompt, workflow, or URL-fetch escape hatch.

The adapter preserves:

- Operation and schema versions.
- Request, idempotency, principal, workspace, and correlation identities.
- Expected revisions and optimistic concurrency.
- Immutable plan references and plan digests.
- Structured reason codes, warnings, evidence references, and degraded states.
- Durable event ordering and resumable job observation.
- Subject, snapshot, namespace, representation, index-generation, and query-provider references.
- Authorization, privacy, data-egress, and audit effects.

If an adapter offers a convenience action, it MUST be transparent composition of documented typed operations. It cannot hide an unreviewed policy change or destructive action inside a UI-only transaction.

## 4. REST transport profile

### 4.1 General requirements

A shipped REST profile MUST:

- Publish an OpenAPI document generated from, or conformance-tested against, canonical operation schemas.
- Version its media types or API surface independently from human-readable UI releases.
- Use structured request and result envelopes rather than scraping CLI prose.
- Support bounded pagination, sorting, filtering, field selection, and cancellation.
- Preserve decimal-string handling for large byte counts and offsets where required by the core schema.
- Use immutable opaque resource references rather than exposing database row IDs or repository locators.
- Enforce request, response, query-complexity, upload, download, concurrency, and rate limits.
- Treat unknown critical fields as errors and define safe behavior for optional fields.

The adapter MAY provide REST-shaped resource routes for convenience, but state-changing requests still resolve to a canonical operation and return its result envelope.

### 4.2 Long-running operations

Planning, ingest, verification, reprocessing, reindexing, restore, migration, and lifecycle work may outlive one HTTP request. The REST profile SHOULD support:

- Immediate operation acceptance with an opaque job reference.
- Durable status reads.
- Ordered event pages after a sequence cursor.
- Server-sent events or WebSocket presentation as an optional optimization.
- Safe cancellation through the canonical bounded cancel operation.
- Reconnection without creating duplicate work.

Correctness cannot depend on a browser, socket, or event stream remaining connected. Exactly one durable terminal result remains available through ordinary status and event reads.

### 4.3 Content transfer

Large content, previews, exports, and restore artifacts MUST use bounded streaming or scoped handles. They MUST NOT be embedded in ordinary JSON plan, search, status, or event responses.

Content reads bind a subject or snapshot, authorized representation, byte range, principal, expiry, and quota. The adapter supports cancellation and integrity reporting and prevents path traversal, confused-deputy access, and reuse under another principal.

The REST adapter MUST NOT concatenate client path strings with host paths or expose repository credentials to create direct download URLs. Any delegated URL is short-lived, narrowly scoped, auditable, and incapable of repository enumeration.

### 4.4 Authentication and remote exposure

The default HTTP profile SHOULD bind only to a Unix socket or loopback interface until remote access is explicitly configured. A remotely reachable profile requires:

- TLS and secure session or token handling.
- Authenticated principals and scoped service accounts.
- Resource-level authorization for sources, snapshots, annotations, search results, content, restores, repositories, processors, and administration.
- CSRF protection for browser sessions and origin controls where applicable.
- Audit records for login, token, policy, plan, plugin, content-read, and destructive events.
- Secret redaction and safe diagnostic export.
- Bounded revocation and session expiry.

Remote access, multitenancy, federation, and high availability are separate deployment qualifications. Merely exposing the local controller on a network does not qualify those modes.

## 5. WebUI product model

The primary UI is outcome-first. Operators should not have to build a node graph to get a good storage and discovery pipeline. Strong presets and automatic routing are the default; advanced configuration edits typed profiles and policy fields.

The UI SHOULD organize the product around these tasks:

### 5.1 Sources and ingest

- Add a local tree, mounted share, snapshot-capable source, or supported object view.
- Show source reachability, consistency profile, last successful ingest, current lag, and observed changes.
- Select a default ingest profile and repository placement.
- Preview which bytes are exact, duplicated, compressed, derived, unknown, unsupported, or pending analysis.
- Review conflicts and explicit fallback behavior before applying a plan.

### 5.2 Storage and recovery

- Show logical protected bytes separately from physical stored bytes and estimated savings.
- Explain whether savings come from deduplication, compression, transformation, prior placement reuse, or exclusion under an explicit later policy.
- Show repository health, placement count, verification evidence, decoder dependencies, and last successful readback.
- Browse the original directory tree independent of physical packs or chunks.
- Restore a file, subtree, or snapshot through a reviewable destination plan.

### 5.3 Search and catalog

- Provide filename, path, type, metadata, tag, note, and extracted-text search from the baseline index.
- Add semantic, visual, audio, or hybrid modes only when compatible providers and healthy generations are active.
- Show which provider and generation produced a result and whether it is stale, incomplete, or approximate.
- Navigate every result to its authoritative namespace entry, versions, annotations, and content access.
- Let authorized users create tags, notes, collections, and corrections without conflating them with processor suggestions.

### 5.4 Processing and extensions

- Show installed processors, repository drivers, index providers, and query providers with versions and health.
- Explain the active default route for a content class in plain language.
- Display queued, failed, degraded, stale, or blocked derivative work.
- Allow safe profile selection, canary activation, reprocess, reindex, rollback, and dependency-aware removal through typed plans.
- Warn when removing a decoder would strand an admitted representation.

### 5.5 Operations

- Show active and recent plans and jobs with resumable progress.
- Separate discovery degradation from recovery risk.
- Surface source change, repository capacity, verification failure, stale index, processor unavailability, and credential expiry.
- Provide clear next actions and preserve the typed reason code for automation.

## 6. Interaction principles

The default experience SHOULD use presets, checkboxes, review tables, filters, and concise explanations. Natural-language assistance MAY explain or draft a proposal, but chat is not the required control surface.

A node or flow visualization MAY help experts inspect a route and dependencies. It is not the canonical configuration model, and the initial UI MUST NOT require users to connect processors manually. Any expert graph editor, if later added, emits the same bounded typed profile and cannot express arbitrary code, cycles, unsupported schemas, or authority transfer.

The UI MUST clearly distinguish:

- Observation from decision.
- Candidate from accepted plan.
- Logical bytes from physical stored bytes.
- Upload receipt from verified readable placement.
- Exact identity from perceptual similarity.
- Recoverable representation from rebuildable derivative.
- Published snapshot from index generation.
- Recovery health from discovery health.

## 7. Human authority and AI presentation

Humans retain final authority for policy changes and destructive actions. The UI may apply safe, predefined defaults and automate accepted recurring profiles, but it cannot infer approval from a checkbox left unchanged, page navigation, chat response, model score, or successful processor result.

An AI assistant in or beside the UI remains an external client. It MAY:

- Explain inventory, savings, risks, and typed reason codes.
- Search and summarize authorized content with subject references.
- Suggest tags, routes, or profile changes.
- Prepare a proposal or request a deterministic plan.
- Monitor work already authorized by the operator.

It MUST NOT gain hidden access to the database, repositories, plugins, secrets, or live filesystem. It cannot mint approvals, select broader source scope, delete the last representation, or mark data verified. The product does not require a built-in assistant; third-party harnesses can use CLI or MCP.

## 8. Index and query presentation

The WebUI must be useful with only the baseline lexical index. It MUST NOT hide ordinary search behind an embedding setup screen or imply that vector search is necessary for a smart catalog.

When semantic or multimodal providers are present, the UI displays:

- Processor model and feature generation.
- Index and query-provider generation.
- Indexed inventory revision and completeness.
- Active versus building, validating, stale, failed, or rollback generation.
- Similarity or ranking explanation at an appropriate level.
- A control to fall back to lexical or structured search.

Reindexing or replacing a query provider does not modify files or recovery records. The UI SHOULD make that separation visible and offer generation comparison before activation.

## 9. UI safety requirements

The UI MUST NOT:

- Mark an upload or index hit as verified recovery.
- Hide unknown, unsupported, unreadable, changing, excluded, or single-placement data.
- Convert a recommendation, caption, model confidence, embedding distance, or external match into an approved omission.
- Mutate a source, restore destination, repository, retention rule, processor activation, or index generation outside a canonical operation.
- Require an AI model or semantic index for exact browse and restore.
- Receive repository administration credentials in browser code.
- Open protected content except through scoped content handles.
- Treat display paths as repository locators or trusted filesystem paths.
- Show unauthorized result counts, facets, snippets, previews, or similarity neighbors.

Destructive or fidelity-changing plans require explicit review of scope, expected revisions, fallback, affected bytes, repository effects, rollback, and authority references. The UI submits the exact reviewed plan digest.

## 10. Accessibility and operational quality

A shipped WebUI profile SHOULD define:

- Supported browsers and responsive layouts suitable for desktop and tablet administration.
- Keyboard operation, semantic markup, screen-reader labels, focus management, and color-independent status.
- Localization boundaries without localizing machine reason codes or opaque identities.
- Large-collection behavior using server-side pagination and virtualization.
- Progressive results that label estimates as provisional or final.
- Reconnection and stale-view handling through explicit resource revisions.
- Safe cache policy for sensitive metadata, previews, and content.

Browser compatibility, remote-service availability, multitenancy, and accessibility qualification belong to the adapter release and MUST NOT become hidden blockers for the headless storage core.

## 11. Conformance and acceptance criteria

1. Removing the REST service and WebUI does not prevent ingest, storage reduction, search, browse, verification, restore, reprocessing, or reindexing through qualified headless surfaces.
2. The same canonical request through CLI, MCP, and REST produces equivalent domain results, plans, reason codes, references, and audit effects after transport metadata is removed.
3. Every UI mutation has an equivalent documented typed core operation.
4. The UI remains useful in an explicitly degraded lexical/structured mode when the local embedding generation is unavailable; that state cannot be presented as the qualified default discovery experience.
5. Large file content and exports use bounded scoped streams or handles rather than ordinary JSON responses.
6. A disconnected browser can resume job observation without duplicating work or losing the durable terminal result.
7. Search results are reauthorized and cannot disclose unauthorized counts, snippets, previews, or neighbors.
8. The default interface offers automatic presets and reviewable options; it does not require a node editor.
9. AI presentation can propose and explain but cannot create authority or bypass the typed operation boundary.
10. A remotely exposed profile passes authentication, authorization, CSRF, rate-limit, path-traversal, content-handle, secret-redaction, and session-revocation tests.
11. Processor, index, query, and repository versions displayed by the UI match the immutable references used by the plan or result.
12. Recovery health, repository placement, derivative availability, and index health are presented as separate states.
