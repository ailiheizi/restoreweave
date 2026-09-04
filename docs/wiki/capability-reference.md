# Capability reference

This page is an operator-oriented reference for the current RestoreWeave
development profile. It explains what a capability means in everyday use; it
does not replace the [MVP and Operator Contract](../requirements/mvp-and-operator-contract.md)
or the [Content Store, Views, and Export Requirements](../requirements/content-store-views-and-exports.md).

## Configuration and paths

RestoreWeave uses one persisted configuration for the content repository,
SQLite catalog, search/index locations, protection defaults, compression, and
semantic-search profile. Paths are resolved to absolute effective paths; the
process working directory is not used implicitly. A supplied `.` is an input
root only when the operator explicitly supplies it.

The normal inspection commands are:

```text
rw config init
rw config validate
rw config show --effective
```

One-shot CLI values take precedence over environment overrides, persisted
configuration, and platform defaults. Effective configuration is redacted and
its digest is recorded wherever a plan, publication, description revision, or
index generation depends on it. Credentials are references, not plaintext
configuration values. See [Phase 0 configuration](../technical/core-mvp-execution-plan.md#phase-0-contract-and-configuration-freeze).

## Exact identity and deduplication

Exact identity is the SHA-256 digest of the complete logical byte stream plus
its logical length. The default protection mode is `STORE_EXACT`: readable
source bytes are retained in the configured repository and can be verified and
restored. Whole-file exact deduplication may avoid storing the same bytes twice;
it never changes what recovery promises.

`LINK_ONLY` is an explicit operator choice. Until a locator is independently
reacquired and verified, its visible state is
`LINK_ONLY_UNPROTECTED`. A filename, metadata fact, embedding, similarity
result, or failed processor cannot establish exact identity or authorize
deletion. Readable but unclassified or processor-failed content follows the
exact-preservation fallback.

Logical duplicate savings, repository compression, and physical capacity are
different measurements. RestoreWeave does not report duplicate bytes as freed
source capacity, and automatic source deletion or destructive garbage
collection is disabled. The full identity and storage rules are in [content
store requirements](../requirements/content-store-views-and-exports.md#4-identity-and-exactness).

## Notes, tags, and descriptions

The interface presents one **Notes** surface. User notes, imported or extracted
text, and model-produced descriptions can appear there with a source label.
Their durable records remain separate when revision, provenance, acceptance, or
recovery evidence requires it.

User organization is multi-tag-first. Durable `Annotation.TAG` facts are user
tags; type and format facets are system facts. “No user tag” is a calculated
coverage/status filter, not a tag silently added by the system. Machine
classification may suggest tags, but must be explicit, attributable, previewed,
and confirmed before changing user-visible tags.

Description generation is an on-demand operation, not an ingest prerequisite.
Unknown or unavailable processors do not prevent exact preservation. See
[descriptions and semantic source material](../requirements/content-store-views-and-exports.md#8-descriptions-and-semantic-source-material)
and [ADR-008](../technical/build-stack-and-architecture-selection.md#adr-008-one-notes-presentation-over-provenance-preserving-text-records).

## Search and BGE status

Lexical and structured search are the baseline and remain useful without a
semantic provider. The intended personal profile uses local ONNX Runtime,
`BAAI/bge-small-zh-v1.5`, and in-process zvec. Search indexes are rebuildable
projections, not recovery authority; rebuilding them does not alter content,
notes, tags, or recovery records.

The current tree has implemented and tested real local BGE/ONNX + zvec loading
for the development bundle, including explicit degraded reporting. A verified
bundle installer and qualification evidence exist, but supported packaging and
release/default qualification remain candidate work. The honest states are:

- `READY`: the required generation is available and queryable;
- `NOT_BUILT`: indexing has not been generated yet;
- `UNAVAILABLE`: the provider, bundle, or compatible generation is not usable.

Semantic unavailability degrades discovery; it does not block exact save,
verification, or restore. Fixture providers are qualification-only and are not
the default capability. See [index status and search](index-status-and-search.md)
and [semantic qualification](../technical/semantic-qualification-results.md).

The fixed development bundle is installed explicitly, never as a hidden
first-query download:

```text
rw semantic bundle install
rw semantic bundle install --archive /absolute/path/to/bundle.tar.gz
```

After installation, restart the daemon and rebuild the semantic generation when
the status page asks for it. The installer is a development convenience, not a
supported release installer; its pinned files and platform limits are recorded
in the [semantic qualification report](../technical/semantic-qualification-results.md).

## Capacity and coverage

Repository capacity is a read-only optional report. If the backend cannot
provide a trustworthy value, RestoreWeave reports `UNKNOWN` (with a reason when
available) rather than blocking an exact save. Per-file tag, lexical, and
semantic coverage are calculated projections, not another durable state store
and not user tags. They may say that a field is missing or an index is
unavailable without changing the protected bytes.

The current development implementation reports these states and keeps logical,
duplicate, compression, physical, overhead, and net accounting distinct. A
production repository choice, representative corpus measurements, and release
qualification remain planned in [Phase 5](../technical/core-mvp-execution-plan.md#9-phase-5-simple-qualified-storage-savings).

## Recovery boundary

The recovery authority is the repository’s exact objects plus authenticated,
portable recovery records. A clean reader can validate the portable closure and
restore exact bytes without the operational SQLite catalog or search indexes.
Names, source provenance, protection outcome, recovery references, and relevant
filesystem facts are retained with explicit degradation where the destination
cannot reproduce a fact.

Search results, embeddings, descriptions, external URLs, and perceptual or
semantic matches are discovery or supporting evidence—not proof of exact
recovery. An external locator remains only a reference until acquisition is
quarantined, independently checked against digest and length, and admitted as
a new exact representation. See [Recovery Fidelity](../requirements/recovery-fidelity.md)
and [Restore Manifest](../requirements/restore-manifest.md).

## What is not finished

The development profile is implemented and tested, but it is not
release-qualified. Still open are supported semantic-model packaging and host
qualification, final production repository selection, broader platform fact
capture (including sparse extent and per-field provenance coverage), broader
representative performance/recall evidence, migration and release hardening,
and the complete `RW-MVP-1` acceptance run. Minimal file-only LinkGroups and
their group-aware export flow are planned for Phase 6; richer Collections,
mount services, automatic reacquisition, destructive deletion, and neural
codec implementations are outside the current core queue.

## Verification entry points

For an operator workflow, start with [Quick start](quick-start.md), then use
`rw doctor`, `rw config validate`, a read-only ingest/plan review, digest-bound
apply, sampled verification, search, recovery export, and an explicit restore
verification. The [CLI and MCP Contract](../requirements/cli-and-mcp-contract.md)
defines command behavior; the [Core MVP Execution Plan](../technical/core-mvp-execution-plan.md)
and [remaining-work matrix](../technical/remaining-work-and-closed-decisions.md)
show the evidence behind each status. A passing fixture or protocol handshake
alone is not release qualification.
