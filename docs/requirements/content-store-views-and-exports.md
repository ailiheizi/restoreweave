# Content Store, Views, and Export Requirements

## 1. Status and purpose

This document is normative for RestoreWeave's durable content model, storage defaults, deduplication claims, discovery derivatives, saved views, exports, and garbage-collection authority. It narrows the product around one principle:

> RestoreWeave stores immutable content and facts about it, then materializes the set a user asks for. A source path is provenance and recovery information; it is not the primary organization of the stored collection.

When an informative implementation plan conflicts with this document, this document wins. Product scope remains owned by [Product Requirements](product-requirements.md), and exact recovery evidence remains owned by [Restore Manifest](restore-manifest.md) and [Recovery Fidelity](recovery-fidelity.md).

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are normative.

### 1.1 Decision lock and status vocabulary

The following decisions are frozen for `RW-MVP-1`. Changing one requires an explicit product-requirement revision and migration analysis; an implementation plan, fixture, benchmark, or adapter test cannot change it.

| Topic | Frozen decision |
| --- | --- |
| Product shape | A content, description, discovery, export, and recovery plane; not a normal writable filesystem or a media application |
| Organization | Stable subjects, durable facts, tags, descriptions, relations, saved views, and frozen exports; original paths are provenance and a recovery projection |
| Exact identity | SHA-256 over the complete logical byte stream plus logical length |
| Default deduplication claim | Exact whole-content deduplication only; repository-private verified chunk deduplication may add savings |
| Protection default | `STORE_EXACT`; `LINK_ONLY` is explicit, confirmed, visibly unprotected until qualified reacquisition succeeds |
| Release storage | One qualified lossless `RepositoryDriver`; no engine is selected merely because a development or candidate profile passes unit tests |
| Current local storage profiles | `directory-cas-dev-v1` is development-only; `local-zstd-v1` is an opt-in, no-service candidate |
| Default discovery | One fused lexical, structured, and local semantic query experience |
| Default semantic profile | Bundled local ONNX/BGE encoder plus in-process zvec; no Docker Compose or separate vector service |
| Semantic degradation | Exact ingest, verification, restore, and lexical/structured search continue, but the installation reports `SEMANTIC_INDEX_UNAVAILABLE` and is not a qualified default experience |
| Long-form knowledge | User, imported, extracted, and generated descriptions are durable versioned documents; vectors are disposable projections of their segments |
| Description generation | Description storage and indexing are core; model-generated descriptions are explicit, on-demand work through a selected `DESCRIBE_SUBJECT` profile, not an automatic ingest dependency or a second bundled model hidden behind the embedding choice |
| Online models | Explicit replacement profiles with egress and credential policy; never required for the personal-use default |
| Output | A dynamic `SavedView` freezes into an immutable `ExportManifest`, which is materialized or streamed on demand |
| Filesystem presentation | RestoreWeave ships no FUSE, SMB, NFS, WebDAV, S3 gateway, or mount API; external tools may consume exports or authorized reads |
| OpenList | May be studied or used externally, but is not a core dependency, storage engine, catalog authority, or codebase to fork for `RW-MVP-1` |
| Neural/RWKV codec | Disabled by default and deferred until simple lossless storage and complete recovery are qualified |

Documents use these status terms precisely:

- **release default**: mandatory behavior of a qualified `RW-MVP-1` distribution;
- **generated default**: the value currently written by `rw config init`, which may remain conservative while the release profile is unfinished;
- **implemented and tested**: working in this checkout for its explicitly stated scope;
- **candidate**: runnable and measurable, but not release-qualified;
- **interface/fixture only**: proves a contract shape, not the user capability;
- **planned**: specified but not implemented;
- **closed non-goal**: deliberately outside the core, not missing work.

The words “complete”, “default”, and “available” MUST be qualified by one of those meanings. In particular, a fixture embedding, protocol handshake, or signed-foundation test cannot close the semantic, user-loop, or clean-recovery release gate.

## 2. Product model

RestoreWeave is a content, annotation, discovery, and recovery plane. Its canonical flow is:

~~~text
source trees / application exports / object sources
    -> capture evidence and original-path provenance
    -> exact ContentID and immutable exact representation
    -> optional representations and rebuildable derivatives
    -> durable tags, notes, relations, and saved queries
    -> frozen ExportManifest
    -> directory / archive / object prefix / protocol facade / foreign tool
~~~

The authoritative collection is not a writable directory tree. A folder-shaped result is one possible materialization of an immutable subject set. The system MAY retain and restore original paths, but daily organization SHOULD be driven by tags, annotations, relations, structured fields, search, and saved views.

RestoreWeave therefore resembles a Nix-style store in four limited ways:

- admitted bytes and representations are immutable;
- plans bind inputs, algorithms, configuration, and outputs;
- user-facing outputs are materialized from immutable records;
- garbage collection follows explicit roots rather than current path reachability.

RestoreWeave is not a package manager, a POSIX filesystem, or a writable synchronization engine.

## 3. Authoritative data layers

The core MUST distinguish these layers:

| Layer | Durable meaning | Authority |
| --- | --- | --- |
| `SourceView` | One bounded observation of an external source, including consistency and capture evidence | Authoritative provenance, not content identity |
| `ContentRecord` | Exact byte length and host-computed cryptographic identity | Authoritative byte identity |
| `Subject` | Stable user-facing identity to which facts and annotations attach | Authoritative catalog identity |
| `Representation` | Exact or derived bytes with kind, fidelity, producer, dependencies, and verification state | Authoritative only for its declared contract |
| `ProtectionRecord` | The selected protection mode and visible recovery outcome for one subject | Authoritative policy and health fact |
| `RecoveryReference` | One ordered, verifiable way to recover or reacquire a subject | Authoritative recovery evidence for its declared claim |
| `MetadataBundle` | Namespaced captured and accepted facts about a file, source, or representation | Durable facts with per-field provenance |
| `DescriptionDocument` | Versioned user, imported, extracted, or model-generated descriptive information | Durable fact with provenance; never exact-byte identity |
| `SemanticSegment` | A bounded searchable segment of a description or extracted document | Durable input to rebuildable semantic indexes |
| `NamespaceEntry` | A name, metadata, and relation observed in one source or snapshot | Authoritative provenance and recovery projection |
| `Annotation` | Versioned user or system-authored tag, note, rating, progress, or other declared fact | Durable; authority is recorded per fact |
| `Relation` | A typed edge between subjects or records, with provenance and confidence | Durable when admitted; never byte identity |
| `SavedView` | A named dynamic query and presentation policy | Durable query, dynamic membership |
| `ExportManifest` | A frozen set of subjects, selected representations, output names, and a canonical digest | Durable, reproducible output intent |
| `Placement` | Evidence that a representation or portable record exists in one repository | Durable storage evidence, not logical identity |
| `IndexGeneration` | A versioned lexical, vector, acoustic, graph, or multimodal projection | Rebuildable derivative |

A `Subject` MAY have many source paths, many annotations, many representations, and membership in many exports. No source path, export path, embedding row, or backend object key may become the subject or content identity.

`NamespaceEntry` preserves the original name and source facts; it does not imply that local bytes are retained. A subject may have a local exact representation, only external recovery references, or both. The absence of a local payload is a visible protection state, never an empty or successful exact representation.

### 3.1 Physical storage simplicity

The logical layers above do not require one physical database per layer. The
personal-use layout SHOULD use one configured SQLite catalog for subjects,
namespace entries, content identities, representations, protection records,
annotations, descriptions, recovery references, GC references, and ordinary
rebuildable search tables. File bytes and admitted representations remain in
the configured content repository because they need streaming, placement,
compression, and verification independent of SQLite row size.

The semantic provider MAY keep provider-owned generation files such as zvec
collections, but they are disposable search projections, never a second
metadata authority. A signed portable recovery export or publication bundle
is an independently retained backup artifact derived from the catalog and
publication records; it is not a second live catalog that ordinary operations
must keep in sync.

## 4. Identity and exactness

### 4.1 Default identity

The default and mandatory exact identity is the tuple:

~~~text
ContentIdentity = (sha256:<lowercase-hex-digest>, byte_length)
~~~

The host computes the digest over the complete logical byte stream. SHA-256 plus length is the only default basis for claiming that two complete files are byte-identical. The digest string remains the compact repository object key; the logical length is mandatory in every `ContentRecord`, `FileVersion`, `Representation`, and recovery receipt. A future serialized ID MAY encode both fields, but it MUST preserve the tuple semantics and MUST NOT silently reinterpret existing digest-only object keys. Additional hashes MAY be stored as derived evidence, but changing or adding them MUST NOT change an existing content identity.

Names, paths, timestamps, file type, metadata, repository checksums, chunks, perceptual hashes, embeddings, model scores, and decoder claims MUST NOT establish exact identity.

### 4.2 Representation classes

Every representation MUST declare exactly one fidelity class:

| Fidelity | Meaning | May replace exact fallback? |
| --- | --- | --- |
| `EXACT_SOURCE` | The original logical bytes | Yes |
| `EXACT_REVERSIBLE` | Different stored bytes with a pinned decoder closure that reproduces `ContentID` | Only after independent decode-and-hash verification |
| `NORMALIZED` | Deterministic normalized content that may differ from the source | No |
| `APPROXIMATE` | Lossy, regenerated, perceptual, or neural output | No |
| `DERIVED_ARTIFACT` | Extracted text, thumbnail, transcript, embedding input, or similar derivative | No |

A transform, including an RWKV- or Transformer-based codec, is a `Processor` output. It is never an identity provider. An exact claim requires a pinned decoder and independent full decode whose result hashes to the source `ContentID`. Otherwise its result remains `APPROXIMATE` or `DERIVED_ARTIFACT`, and the exact fallback remains retained.

### 4.3 Protection modes and recovery references

Every subject MUST have one explicit `ProtectionRecord`:

| Mode | Meaning | Permitted health claim |
| --- | --- | --- |
| `STORE_EXACT` | A local exact representation is retained and independently verifiable | `EXACT_PROTECTED`, or `EXACT_FALLBACK` when identification/optional processing is unresolved |
| `STORE_EXACT_WITH_EXTERNAL_FALLBACK` | Local exact bytes exist, plus one or more external references | `EXACT_PROTECTED` or `EXACT_FALLBACK`; external references remain unvalidated fallback routes |
| `LINK_ONLY` | No local payload is retained; one or more external references are retained | `EXTERNAL_REPLAYABLE` or `LINK_ONLY_UNPROTECTED`, never `RESTORE_VERIFIED` |
| `METADATA_ONLY` | No exact payload or verified external reference exists | `EXPLICITLY_UNPROTECTED` only after an explicit human or already-published policy decision; otherwise `BLOCKED` or `UNAVAILABLE` |

`LINK_ONLY` MUST require an explicit plan decision when the source bytes are readable. It MUST preserve the original name, all captured metadata, expected content identity when known, and a visible warning that recovery requires reacquisition. A URL, search result, embedding match, or mutable provider label alone is not a recovery reference.

One `RecoveryReference` records at least:

- reference ID, subject, kind, priority, and recovery claim;
- expected content identity and logical length when known;
- exact or reversible representation reference when local bytes exist;
- external `SourceBinding` and one or more typed locators when reacquisition is required;
- decoder/codec profile and dependency closure for reversible representations;
- verification evidence, last cold validation, expiry, and current status;
- policy, rights, credential-reference, and operator-decision references.

The supported reference kinds are `EXACT_REPRESENTATION`, `EXACT_REVERSIBLE`, `EXTERNAL_LOCATOR`, and `USER_RECIPE`. Multiple references are ordered alternatives, not duplicate identities. Reacquisition always downloads into quarantine, independently hashes the result, and creates a new local representation; it never mutates a historical link-only record into pretending that bytes were always protected.

Every published exact, reversible, or link-only subject MUST contain at least one portable `RecoveryReference` in the authenticated closure. The core MUST be able to export, on demand, one or more signed `RecoveryToken` values for any such subject. Tokens need not be pre-generated or stored as separate rows: they are deterministic proof envelopes over an admitted reference, its canonical recipe or locator-set digest, expected identity, publication, and trust-anchor reference. A token is a pointer and proof envelope, not the compressed payload and not a substitute for the repository. A subject may have several tokens for independent placements, codecs, or external sources. A metadata-only subject has no recovery token because it has no recovery path; its `EXPLICITLY_UNPROTECTED` outcome is exported instead.

`ProtectionMode` is the requested policy; `ProtectionOutcome` is what the system actually achieved. The canonical outcomes are:

| Outcome | Meaning | Publication rule |
| --- | --- | --- |
| `EXACT_PROTECTED` | A local exact representation passed placement and readback requirements | May publish as protected |
| `EXACT_FALLBACK` | Readable bytes are exact, but identification or optional processing is unresolved or failed | May publish as protected with the unresolved reason visible |
| `EXTERNAL_REPLAYABLE` | No local payload is selected, but a qualified immutable external reference has passed the required acquisition and cold-verification profile | Later profile only; never inferred from a URL alone |
| `LINK_ONLY_UNPROTECTED` | One or more locators are recorded, but no currently qualified exact recovery path exists | May publish only after explicit confirmation; never report restore verified |
| `EXPLICITLY_UNPROTECTED` | The operator accepted metadata-only retention for a narrowly admissible case | Publish the decision and loss of byte recovery explicitly |
| `BLOCKED` | Required evidence is unresolved, conflicting, unsafe, or unstable | The plan is non-executable and cannot publish |
| `UNAVAILABLE` | A retained subject or reference currently has no usable recovery path | Remains visible and unhealthy; cannot satisfy recovery coverage |

No other document may redefine these values. Human labels may be friendlier, but machine output and acceptance tests use the canonical names.

Recovery-reference selection follows an explicit state machine:

~~~text
RECORDED
  -> LOCALLY_VERIFIED | EXTERNALLY_VALIDATED
  -> COLD_VERIFIED
  -> EXPIRED | UNAVAILABLE | FAILED
~~~

The core considers references in declared priority order, but it MUST NOT perform network access, disclose credentials, or accept a weaker fidelity class merely by falling through the list. Local exact alternatives may be tried automatically within the same authorized restore plan. An external reference requires a separately authorized acquisition step into quarantine; success creates and verifies a new local exact representation before restore. A failed or expired reference remains in history with evidence and does not disappear.

A `RecoveryToken` MUST bind its schema, publication domain, recovery-reference ID, expected identity and length, recipe digest when applicable, expiry or no-expiry declaration, and trust-anchor reference. Losing the token does not destroy repository data; losing the repository means the token alone contains no payload; losing the independent trust anchor prevents the token or publication signature from being trusted. Operator output MUST explain those cases separately.

### 4.4 Reversible codec recipe

An `EXACT_REVERSIBLE` representation MUST carry a portable `RecoveryRecipe` containing the codec/profile ID, implementation digest, model/tokenizer/dictionary digests, arithmetic or range-coder profile, framing/chunk index digest, encoded payload placement, decoded length, and validator profile. The required proof is:

~~~text
encoded placement -> pinned decoder -> complete byte stream -> SHA-256 + length == source ContentIdentity
~~~

RWKV or Transformer prediction plus arithmetic coding is a valid future codec shape, but the model supplies probabilities and the entropy coder preserves symbols. It is not the default storage method, it does not establish identity, and it cannot retire the exact fallback until repeated cold decode, corruption, upgrade, and clean-install tests pass. The first storage profile uses mature raw/lossless repository compression; codec experiments remain replaceable processors.

A codec profile additionally declares accepted content classes, block and random-access behavior, encoded and decoded size bounds, peak CPU/RAM/scratch limits, model availability and redistribution policy, corruption detection, decoder support window, migration path, and the minimum measured net saving required for admission. A representation that cannot satisfy the declared decoder closure remains experimental and cannot become the sole recovery reference.

### 4.5 Minimal portable file-fact profile

Every captured namespace entry MUST preserve a portable fact record even when no local payload is retained. Each field carries a value when observed plus `source_profile`, `authority`, `capture_time`, provenance digest, and one state: `OBSERVED`, `UNOBSERVED`, `UNSUPPORTED`, `REDACTED`, `INCONSISTENT`, or `NOT_APPLICABLE`. Missing support is represented as data; it is never silently serialized as a zero value.

| Fact group | Required portable facts | Restore behavior when unavailable |
| --- | --- | --- |
| Name and relation | Raw name bytes, safe display name, raw parent/component relation, entry type | Missing raw name or parent relation blocks fidelity-complete publication |
| Identity and size | Content identity when readable, logical size, allocated size when available | Missing content identity prevents exact-byte claims; unavailable allocated size is reported |
| Time | At least modification time plus every captured creation/change/access time with precision and clock semantics | Restore applies supported times and reports every degradation |
| Ownership and mode | Mode bits, uid/gid or platform identity, link count, source profile | Unsupported ownership or mode is explicit and profile-scoped |
| Link semantics | Symlink target raw bytes, hard-link group, cycle/boundary evidence | Restore never follows a manifest-provided symlink parent; unsupported hard links become distinct copies only under an explicit degradation policy |
| Allocation | Sparse extents or an explicit unknown/unsupported state | Restored bytes remain exact; physical sparseness may degrade only with a visible receipt |
| Extended metadata | xattrs, ACLs, alternate streams, resource forks, flags, and namespaced platform facts | Unsupported fields are retained as portable facts or declared omissions; no silent loss |
| Detection and processing | Suffix, magic/structural evidence, conflicts, processor attempts, warnings, and coverage | Never changes exact identity; unresolved readable content becomes `EXACT_FALLBACK` |
| Protection and recovery | Requested mode, achieved outcome, representation IDs, ordered recovery references and locator states | Missing required recovery evidence blocks a healthy claim |

The qualified platform profile defines which optional filesystem facts it can observe and restore. The portable schema remains platform-neutral, so an unsupported destination can report degradation without discarding the source fact. All recovery-relevant facts, including explicit unsupported states, MUST enter the authenticated closure before the clean-recovery gate can close.

## 5. Storage model and locations

### 5.1 Logical stores

A deployment has one operational metadata database and one content repository.
Search projections may be tables in the database or provider-owned cache files;
they are never a second metadata authority. There is no separate live recovery
database.

| Store | Contains | Recovery authority |
| --- | --- | --- |
| Operational catalog | One SQLite database containing subjects, source observations, plans, annotations, placements, recovery references, and rebuildable search tables | Daily source of metadata; signed publication/recovery artifacts remain the independent recovery proof |
| Content repository | Exact and admitted derived representations plus signed portable recovery/backup artifacts | Yes, subject to receipts and verification |
| Index stores | Lexical, vector, acoustic, graph, and multimodal generations | No; they are disposable and rebuildable |

An export destination is not a fourth authoritative store unless it is admitted as a repository placement. It is normally a materialized output that can be deleted and reproduced from its `ExportManifest`.

### 5.2 Path rules

Source paths and storage paths are never inferred from each other.

- `rw ingest .` MAY explicitly choose the current directory as a source. Omitting a source MUST NOT silently mean the current directory.
- The daemon's catalog and repository paths MUST come from deployment configuration. They MUST NOT default to the client's current working directory.
- Restore and export destinations MUST be explicit and MUST pass destination-specific planning and overwrite checks.
- A repository target SHOULD use an absolute path or a backend URI. Relative daemon configuration MUST be resolved once at startup and reported as an absolute effective value.

The current development daemon loads the strict persisted profile from `RESTOREWEAVE_CONFIG` or the platform config path. One-shot flags override environment path variables, which override persisted values, which override platform XDG defaults. `--catalog` and `--repository` remain one-shot overrides. This behavior is implementation status, not qualification of the in-tree directory CAS as a release repository.

An installer MAY ask for one convenience `data_root`, but `restoreweave.config.v1` has no ambiguous catch-all path. The installer expands that choice into the explicit `paths.*` values below. `paths.recovery_records` is the destination for signed backup/export artifacts, not a second live metadata database. Runtime-only directories may be placed beneath an implementation-owned state root, but they do not become recovery authority.

| Location | Contents | Authority, backup, and deletion rule |
| --- | --- | --- |
| `paths.repository` | Exact/admitted payloads and portable signed repository records | Recovery-critical; back up according to the selected repository profile; never delete by path reachability alone |
| `paths.recovery_records` | Local publication signing material, exported public anchor material, and recovery workflow state | Security-critical; private material follows key-backup policy and the public trust anchor MUST also be retained independently |
| `paths.catalog` | Operational SQLite subjects, plans, placements, annotations, descriptions, and projections | Target architecture is rebuildable from portable authoritative records, but the current checkout does not yet export every durable fact; do not treat it as disposable until the clean-import gate passes |
| `paths.vectors` | zvec and other semantic generations | Disposable and rebuildable; exclude from recovery authority, but coordinate deletion through generation lifecycle |
| implementation staging | Temporary placement, processor, export, and import objects | Not authoritative; collect only after leases/fences prove no active operation needs them |
| implementation locks | Single-writer, lease, and fencing state | Runtime coordination only; never use a stale lock file as publication proof |
| source path | User-owned input observed through a capture profile | Outside the RestoreWeave data root; never inferred from any storage path |

Configured paths MUST be non-overlapping after resolution. A repository may own private `tmp`, index, pack, or lock paths required by its adapter, but their layout is not a portable API. The controller reports the effective absolute local paths or backend URI and the selected profile; it never derives them from the client `pwd`.

### 5.3 Default repository contract

The release default MUST be one qualified, lossless `RepositoryDriver` profile that provides immutable placement, bounded readback, independent SHA-256 verification, reconciliation, restore access, and portable receipts. Backend-private compression, chunking, encryption, packing, and physical garbage collection remain behind the driver.

The in-tree `directory-cas-dev-v1` profile is a development and test driver. It stores raw whole-file blobs addressed by SHA-256 and does not qualify as the release default merely because exact restore tests pass.

The in-tree `local-zstd-v1` + `zstd-v1` profile is a personal-use candidate, not the release profile. It is embedded in the daemon, needs no Compose stack or separate service, preserves logical SHA-256 identity, eliminates duplicate whole-file objects, and stores payloads as checksummed zstd frames. Portable recovery records remain uncompressed JSON. A separate `local-zstd-encrypted-v1` experimental profile adds an injected host-owned key provider, non-secret key references, AES-256-GCM payload protection, authenticated decode-and-hash readback, missing/wrong-key fail-closed health, clean-install reader dependency, relocation, corruption, and key-rotation copy-forward evidence without placing secrets in plans or portable records. Neither in-tree profile is release-qualified: content-defined chunk deduplication, destructive GC, broader crash/migration rollback, representative corpus measurements, packaging, and complete reader closure remain open. An explicit host-owned repair seam, copy-forward profile migration helper, and mechanism-separated savings measurement are implemented and tested, but these do not establish a production qualification claim.

No repository product or engine name is normative until a dated qualification record binds its version, configuration, corpus, failure tests, readback results, migration path, and license obligations.

### 5.4 Deployment configuration

The effective configuration MUST be printable as redacted JSON and MUST be bound into plans. `restoreweave.config.v1` uses only the following canonical top-level keys; dotted names in this table are exact YAML paths, not aliases:

| Canonical key | Meaning | Generated/current value |
| --- | --- | --- |
| `paths.catalog` | Operational SQLite path | Platform XDG data path |
| `paths.repository` | Exact repository path or supported URI | Platform XDG data path |
| `paths.vectors` | Vector-generation root | Platform XDG data path |
| `paths.recovery_records` | Signing/recovery state path | Platform XDG data path |
| `storage.repository_profile` | Repository adapter and physical-format profile | `directory-cas-dev-v1` in the current checkout |
| `storage.compression_profile` | Compression tuple paired with the repository profile | `identity-v1` for the generated development profile |
| `storage.default_protection` | Default per-item protection decision | `STORE_EXACT` |
| `storage.allow_link_only` | Whether explicit link-only plans are permitted | `true` |
| `storage.link_only_requires_confirmation` | Prevent silent loss of local protection | `true` |
| `storage.neural_codec` | Optional reversible neural-codec profile | `disabled` |
| `semantic.embedding_mode` | `local`, `online`, or `hybrid` provider selection | `local` |
| `semantic.local_profile` | Named local model/runtime/profile manifest | `bge-small-zh-v1.5` target profile |
| `semantic.online_profile` | Named remote provider profile | Empty unless explicitly selected |
| `semantic.online_credential_ref` | Host secret-store reference, never the secret | Empty unless required |
| `semantic.vector_backend` | Vector `IndexProvider`/`QueryProvider` profile | `zvec` target profile |
| `semantic.send_content_without_confirmation` | Remote egress safety switch | `false` |
| `descriptions.enabled` | Persist and index admitted description revisions | `true` |
| `descriptions.generate` | `on_ingest`, `on_demand`, or `disabled` model generation policy | `on_demand` |
| `descriptions.provider_profile` | Explicit `DESCRIBE_SUBJECT` profile; unrelated to the embedding profile | Empty until selected |
| `descriptions.credential_ref` | Host secret-store reference for a remote description profile | Empty unless required |
| `descriptions.retain_full_text` | Retain the durable source text from which segments are derived | `true` |
| `recovery.require_exact_fallback` | Prevent processing failure from weakening readable-byte protection | `true` |
| `recovery.allow_external_reacquisition` | Permit a later qualified `RetrieverDriver` plan | `false` |
| `recovery.publication_signing` | Publication signature profile | `local-ed25519-v1` |
| `recovery.publication_domain` | Domain separation for publications and tokens | `workspace:default` |

The named local semantic profile manifest, not additional ad hoc config keys, pins the ONNX Runtime artifact, model and tokenizer digests, preprocessing, prefixes, token limit, pooling, normalization, dimension, metric, zvec native library, Go binding, schema, index parameters, licenses, and platform tuple. An online profile manifest similarly pins endpoint, provider/model revision, egress scope, retention declaration, score semantics, and failure policy. The config chooses a profile; it does not duplicate or partially override its immutable meaning.

First-run onboarding SHOULD require only three operator choices: the data location (expanded to `paths.*`), repository profile, and embedding profile. Protection, description, and recovery settings have conservative defaults but remain visible and editable. Export destinations and per-item `LINK_ONLY` decisions are operation inputs, not hidden global paths.

The first user-facing configuration MUST be a persisted, versioned file. It SHOULD use a human-editable format with a strict schema; the reference implementation uses `config.toml` and emits a redacted canonical JSON view for diagnostics. The default file is `$XDG_CONFIG_HOME/restoreweave/config.toml` (or the platform equivalent), while data paths remain independently configurable. Older `.yaml` profiles remain readable for migration, but new files are written as TOML. A minimal profile is:

~~~toml
schema = "restoreweave.config.v1"
[paths]
catalog = "/data/restoreweave/catalog.sqlite"
repository = "/data/restoreweave/repository"
vectors = "/data/restoreweave/vectors"
recovery_records = "/data/restoreweave/recovery"
[storage]
repository_profile = "directory-cas-dev-v1"
default_protection = "STORE_EXACT"
allow_link_only = true
link_only_requires_confirmation = true
compression_profile = "identity-v1"
neural_codec = "disabled"
[semantic]
embedding_mode = "local"       # local | online | hybrid
local_profile = "bge-small-zh-v1.5"
online_profile = ""
online_credential_ref = ""
vector_backend = "zvec"
send_content_without_confirmation = false
[descriptions]
enabled = true
generate = "on_demand"          # on_ingest | on_demand | disabled
provider_profile = ""            # select a separate local or online generator explicitly
credential_ref = ""
retain_full_text = true
[recovery]
require_exact_fallback = true
allow_external_reacquisition = false
publication_signing = "local-ed25519-v1"
publication_domain = "workspace:default"
[api]
enabled = false
listen = "127.0.0.1:4534"
~~~

To opt into the local compression candidate, change the storage tuple together:

~~~toml
[storage]
repository_profile = "local-zstd-v1"
default_protection = "STORE_EXACT"
allow_link_only = true
link_only_requires_confirmation = true
compression_profile = "zstd-v1"
neural_codec = "disabled"
~~~

Secrets MUST be referenced by `credential_ref` or the host secret store, never placed in this file. `rw config init`, `rw config validate`, and `rw config show --effective` are part of the core operator surface. CLI flags and environment variables (`RESTOREWEAVE_CONFIG`, `RESTOREWEAVE_CATALOG`, `RESTOREWEAVE_REPOSITORY`) may override paths for one invocation. An override is reported and included in the effective config digest; it does not silently rewrite the persisted file.

The HTTP adapter is disabled by default. Setting `api.enabled = true` binds the
same typed command dispatcher at `/api/v1/command`; `restoreweaved --api-listen`
is a one-shot listener override. Keep it on loopback unless an outer proxy
provides authentication and transport policy. `RESTOREWEAVE_API_TOKEN` (or
`--api-token`) supplies an optional bearer token and is never written to TOML,
SQLite, plans, or recovery records.

`descriptions.enabled: true` enables durable user, imported, and extracted descriptions even when `provider_profile` is empty. With `generate: on_demand`, no model runs during ingest. A generation request without a selected healthy profile returns `DESCRIPTION_PROVIDER_UNAVAILABLE` and does not alter the document or semantic-index state. `on_ingest` is accepted only with a pinned, installed profile and an explicit resource/egress policy; its failure degrades description coverage but never exact protection or the required embedding generation. The default checkout keeps `provider_profile` empty until an explicit `DESCRIBE_SUBJECT` profile is admitted; it never infers a provider from the embedding profile.

The same rule applies to external locators. Portable snapshots, recovery exports, audit details, and index feeds MUST NOT contain URI userinfo, bearer or signed query parameters, secret fragments, or a credential-bearing display locator. Access material is resolved from `credential_ref` at execution time. Until a qualified `RetrieverDriver` defines and validates typed public query parameters, the reference recorder rejects all locator query strings and fragments rather than trying to recognize secrets with a denylist.

Every plan, representation, description revision, index generation, and publication MUST bind a `config_digest` and the relevant provider/profile digests. Editing the configuration affects new work only; it never silently changes the meaning of existing records. The daemon MUST refuse an unknown schema, an unsafe path collision, a missing required model/codec profile, or a link-only policy that lacks explicit confirmation rules.

The `config_digest` algorithm is:

~~~text
strict schema decode
-> expand all defaults
-> apply persisted values, then environment overrides, then one-shot CLI overrides
-> resolve relative local paths against the config file directory
-> retain credential reference IDs but exclude resolved secret values
-> canonical JSON using the schema field names and deterministic key ordering
-> SHA-256 over that canonical JSON; the required `schema` field supplies the `restoreweave.config.v1` domain
~~~

Symlink or filesystem-object identity is not folded into the textual config digest; the apply plan separately binds and revalidates the resolved repository/source identities. Model, tokenizer, runtime, codec, and vector-library bytes are bound through immutable provider/profile digests referenced by the effective config. This separation lets credentials rotate without changing algorithm identity while preventing an unpinned model alias from reinterpreting old work.

The current daemon reads this persisted schema and exposes `--config`, `--catalog`, `--repository`, `RESTOREWEAVE_CONFIG`, `RESTOREWEAVE_CATALOG`, `RESTOREWEAVE_REPOSITORY`, `RESTOREWEAVE_VECTORS`, and `RESTOREWEAVE_RECOVERY_RECORDS`. It accepts exactly two implemented storage tuples: `directory-cas-dev-v1` + `identity-v1`, and `local-zstd-v1` + `zstd-v1`; both require `neural_codec: disabled`. A tuple mismatch or unknown profile fails validation/startup instead of being silently ignored. The generated default remains the raw development tuple until the zstd candidate or a mature engine passes the release gates in section 5.3. Repository roots carry an immutable profile marker; a non-empty historical repository without a marker is admitted only as the legacy raw profile and cannot be relabeled zstd without explicit migration. `rw ingest --protection` can override the configured protection decision for one tree, and `--confirm-link-only` plus repeated `--locator [relative-path=]URI` bind explicit external references. Embedding and description provider execution are not implemented merely because their profiles validate. A path supplied for the source is an input only. It does not change the repository, catalog, vector, or recovery-record paths.

### 5.5 Packaging and process model

The personal-use default MUST NOT require Docker or Docker Compose. A supported native installation consists of one supervised `restoreweaved` service, bundled native libraries and model assets, and explicit data directories. The daemon owns the lifecycle of its isolated local embedding worker; zvec runs in-process and does not expose a network listener.

A Compose file MAY package the same profile for NAS and container-oriented operators. It MUST use the same configuration schema, data layout, health checks, backup exclusions, and upgrade rules as the native package. Compose is a delivery option, not a semantic dependency and not a reason to split embedded providers into network services.

External Qdrant, Milvus, remote model serving, and distributed repository services belong to separately named deployment profiles. Selecting one replaces a provider through the same interface; it does not change portable content, annotations, saved views, or export manifests.

## 6. Deduplication and storage reduction

RestoreWeave MUST report each mechanism separately.

| Mechanism | Default | Exactness | Owner |
| --- | --- | --- | --- |
| Whole-content duplicate elimination by logical SHA-256 + length | Enabled in both in-tree profiles | Exact | Core identity plus repository placement policy |
| Repository-private chunk or content-defined deduplication | Repository-profile specific | Exact only when chunks are cryptographically verified | `RepositoryDriver` |
| Lossless compression | `local-zstd-v1` candidate available; required in the eventual qualified release profile | Exact after decode-and-hash qualification | `RepositoryDriver` or admitted exact transform |
| Domain-specific reversible transform | Disabled | Exact only after decoder closure and full verification | `Processor` plus core admission |
| RWKV/Transformer/neural representation | Disabled | Exact only under the same reversible proof; otherwise approximate | `Processor` |
| Perceptual or embedding similarity grouping | Optional | Never deduplication and never exact identity | Processor/index/query providers |

The default user-visible claim is whole-content exact deduplication. Selecting a repository profile MAY add chunk-level savings without changing `ContentID`. The local zstd candidate reports logical and physical bytes separately in its placement receipt, but the current CLI does not yet provide a complete repository-growth or net-savings report. The controller MUST expose which layer produced a savings estimate and MUST NOT add source bytes, logical duplicate savings, compression savings, and physical stored bytes as if they were interchangeable measures.

Similarity MAY help a user review near-duplicates. It MUST NOT automatically delete, alias, or replace one exact subject with another.

The default storage policy is intentionally simple: SHA-256 identity, whole-content duplicate elimination, and a mature lossless repository codec. Content-defined chunking MAY be added inside the qualified repository profile. RWKV/Transformer codecs are opt-in later profiles and are never silently selected by the optimizer.

## 7. Saved views and exports

### 7.1 SavedView

A `SavedView` is a dynamic query. It MUST bind:

- a stable view ID and revision;
- a canonical query AST, not only display text;
- scope, authorization policy, and subject kinds;
- requested fields, sort, grouping, and optional output naming policy;
- required and optional index capabilities;
- behavior when a capability is unavailable;
- creator, timestamps, and provenance.

Examples include `tag:novel AND language:zh`, `duplicate_count > 1`, or `note contains "grandmother"`. Re-running a saved view MAY return different subjects as annotations or index generations change. A saved view alone is not a reproducible export and is not a garbage-collection root.

### 7.2 ExportManifest

An `ExportManifest` freezes one view evaluation or explicit selection. It MUST include:

- immutable subject and source-revision references;
- the selected representation for each output;
- output-relative names and collision decisions;
- requested metadata and sidecar policy;
- decoder and dependency closure when needed;
- target profile and materialization options;
- authorization and policy decision references;
- canonical manifest digest;
- verification requirements and final materialization receipts.

The manifest, not a live query, is the unit of plan, review, apply, retry, verify, and reproduction. Materializing the same manifest to a directory, archive, object prefix, WebDAV surface, OPDS feed, OpenSubsonic facade, or other adapter MUST preserve subject and representation identity even when presentation names differ.

Path-shaped output is therefore a materialization result, not the primary catalog. RestoreWeave MUST NOT implement a mount API or kernel-filesystem adapter. External tools MAY mount or share a restored directory or separately consume authorized export/read contracts, but that behavior is outside the product.

### 7.3 Output integration boundary

The core MUST initially expose output integration through stable data contracts rather than a privileged plugin that can query private catalog tables:

- enumerate a frozen `ExportManifest`;
- resolve authorized `SubjectRef` and `RepresentationRef` values;
- open bounded or streaming `FileAccess` handles;
- report per-item and final receipts;
- verify output against the manifest.

Directory and archive materializers MAY run in the reference process. Protocol facades and external tools MAY consume the same contracts. A public `ExportAdapter` ABI should be standardized only after at least two materially different adapters prove that these contracts are insufficient.

## 8. Descriptions and semantic source material

File names and extracted bytes are necessary but not sufficient for the catalog. RestoreWeave MUST be able to retain namespaced facts and long-form information about a subject without confusing either with the original file. A `MetadataBundle` contains typed key/value facts such as duration, language, platform, edition, dimensions, checksum evidence, or user-defined fields. Every field records its namespace, source, confidence/authority, and provenance. A `DescriptionDocument` is a durable, versioned document attached to a `Subject` and records:

- document kind: `USER`, `IMPORTED`, `EXTRACTED`, `AI_SUMMARY`, or `AI_ANALYSIS`;
- language, title, body, body digest, visibility, and author/producer;
- source artifact or source-binding references and covered source spans;
- model/provider/profile digest when generated;
- confidence, coverage, acceptance state, predecessor revision, and timestamps.

The complete description body MUST be retained in the content/recovery data plane or an admitted artifact placement. It MUST NOT exist only in a vector store. User descriptions and model descriptions are separate revisions; a model result never overwrites a user fact. A generated description is evidence or interpretation, not a recovery claim.

Long documents MUST be split into ordered `SemanticSegment` records with stable segment IDs, source offsets or section labels, language, and parent document revision. Embeddings index segments, then the query broker aggregates hits to the parent `Subject`. Search results SHOULD expose the matched segment, document kind, producer, and acceptance state so a user can tell whether a result came from a filename, extracted text, user note, or model-generated plot/analysis.

A description provider may produce summaries, plot/character/timeline facts, game observations, or other domain-specific text when the source material supports it. The system MUST label model-generated content, preserve the input and model provenance, and never claim that a generated description contains information unavailable in the retained source or supplied by the user.

The reference description profile accepts valid UTF-8 bodies up to 16 MiB per revision. The retained body digest is over the submitted UTF-8 bytes; embedding-specific Unicode normalization, prefixes, truncation, or tokenization are preprocessing facts and never rewrite the durable document. Imported plain text and Markdown retain their declared media type and source reference. HTML or another active format is stored as untrusted source text and MUST be sanitized by a presentation adapter before rendering.

Each accepted revision produces one ordered segment set under a versioned segmentation profile. The current reference segmentation profile:

- uses source byte spans `[start_byte,end_byte)` over the retained UTF-8 body;
- prefers UTF-8-safe sentence or whitespace boundaries;
- limits a segment to 1024 source bytes where practical;
- uses no hidden text and records any truncation or unsplittable span;
- binds document revision, segmentation-profile digest, ordinal, span, language/section label, and segment-text digest into segment identity.

Changing segmentation creates a new segment generation; it does not mutate historical spans. Superseding or tombstoning a description revision makes its segments ineligible for new active generations but does not rewrite an already frozen export or erase audit history. Deleting a vector generation never deletes a description revision or its segments.

User-authored, imported, extracted, and model-generated revisions do not silently override one another. Presentation and ranking may prefer an accepted user revision, but results MUST expose kind, author/producer, source, acceptance state, and matched span. An online description provider receives only the explicitly authorized text/artifact handles and declared metadata fields; the invocation records the egress scope and receipt. Source bytes, private notes, and existing descriptions are denied by default rather than inferred as provider input.

“Search all information” means coverage, not one universal vector. Every authorized durable text source selected by the active profile MUST be either present in the lexical feed and, where semantically eligible, the segment embedding feed, or reported with a typed exclusion/degraded reason. Typed metadata remains available to structured filters even when embedding it would be misleading. Coverage is reported by source kind, field, revision, and active generation.

## 9. Search and embeddings

### 9.1 Lexical and structured fallback

The default product MUST remain recoverable with every model runtime and vector store disabled. The baseline lexical and structured generation remains usable during semantic degradation. Its target fields are original names and paths, detected type, complete captured metadata, exact checksum and length, duplicate group, protection/recovery status, external locator metadata, durable tags and notes, processing state, description documents, and admitted extracted text.

An implementation MUST report field-level coverage. It MUST NOT claim the complete baseline when fields are absent from its index schema or feed.

### 9.2 Embedding lifecycle

An embedding is a `REBUILDABLE_DERIVATIVE`. It MUST NOT be stored as an exact representation, recovery dependency, durable annotation, or content identity. Each embedding generation MUST bind:

- processor capability and implementation digest;
- model artifact and license identity;
- tokenizer and preprocessing digests;
- input artifact and source revision;
- vector element type, dimension, normalization, and pooling;
- semantic space ID and compatibility digest;
- distance function and score interpretation;
- index provider, query provider, and configuration digests;
- generation state, coverage, and deletion/rebuild procedure.

Vectors from different semantic spaces MUST NOT be compared or merged merely because dimensions match. Rebuilding or deleting an embedding generation MUST NOT affect exact content, saved annotations, export manifests, or restore.

The default query broker combines three separately evidenced dimensions:

1. lexical: original filename/path, extracted text, descriptions, tags, notes, URLs, and checksum text;
2. structured: type, size, time, language, metadata, duplicate group, source, protection state, and processing state;
3. semantic: segment-level vectors for filenames, descriptions, extracted text, notes, and other explicitly admitted text.

No single vector is required to contain every fact. A query may match a segment and return its parent subject, while preserving the segment and source provenance in the result.

### 9.3 Default local profile

The reference `RW-MVP-1` distribution MUST enable a local embedding profile by default. The default query broker combines lexical, structured, and semantic dimensions; a user does not need to select an index dimension to receive useful results. Embeddings are not the only search mechanism, but they are a required part of the default experience.

The default local text profile is:

- an isolated processor using the ONNX Runtime 1.29.x C API;
- `github.com/yalue/onnxruntime_go` pinned by commit inside that worker as the reference Go binding, not as a core ABI or daemon-wide dependency;
- a pinned ONNX export of `BAAI/bge-small-zh-v1.5` as the default Chinese/English model;
- model-specific tokenizer, prefix, maximum-token, pooling, normalization, dimension, and cosine-distance configuration bound into one profile digest.

The release package MUST ship or install the model, tokenizer, native runtime, and profile manifest as one qualified bundle. The bundle is not allowed to silently download a model at first query. It becomes release-qualified only after model redistribution, native-library packaging, deterministic fixture, recall, latency, memory, upgrade, and deletion/rebuild gates pass. Other local runtimes, remote services, and models remain valid implementations of the same `Processor` contract, but replacing the default creates a new profile and generation.

The default local vector store is zvec v0.6.x, loaded in-process through `github.com/zvec-ai/zvec-go` v0.6.x, behind the `IndexProvider` and `QueryProvider` seams. One collection directory is created for each semantic-space and index generation. The zvec native library digest, Go binding, collection schema, distance function, index parameters, quantization, and data directory are pinned in the profile digest. A flat exact scan is the conformance reference; HNSW or DiskANN MAY be selected by a measured local profile. No private document ID may appear in portable data.

The reference package MUST bundle the matching native zvec library and MUST NOT download it at first query. Its qualification covers clean install, WAL crash recovery, single-writer ownership, concurrent readers, generation swap, rebuild, deletion, backup exclusion, memory bounds, and upgrades on every supported platform. zvec index data remains disposable even though it is locally durable.

The reference package records zvec and `zvec-go` as Apache-2.0 dependencies in its NOTICE/SBOM. Native library and model licenses, platform archives, and redistribution terms are part of the profile digest and release qualification.

Qdrant and Milvus are explicitly not personal-use defaults. A service-backed profile is justified only by a separately qualified multi-process, multi-node, high-throughput, or multi-tenant deployment; neither is an `RW-MVP-1` dependency.

If the default model or vector generation is unavailable, ingest and exact recovery MUST continue and the result MUST be visibly marked `SEMANTIC_INDEX_UNAVAILABLE`; lexical and structured dimensions remain usable. A release build is not qualified as the default experience until a clean installation can build the semantic generation without network access.

Local and online embedding providers are interchangeable profiles, not two different identity systems. The personal-use default is local ONNX/BGE. An online or hybrid profile MUST declare its endpoint, model revision, credential reference, egress scope, retention policy, request/response digest, and failure behavior. It MUST NOT send source bytes or private descriptions without an explicit policy grant. Switching providers creates a new semantic generation; it does not rewrite old vectors or descriptions. A description provider is separately identified because an embedding model does not necessarily generate long-form descriptions.

`embedding_mode` has fixed orchestration semantics. `local` builds and queries only the selected local semantic space. `online` is an explicit replacement profile and is never chosen because the local worker failed. `hybrid` builds separate local and online generations, queries each generation independently, and lets the host broker fuse subject-bound candidates with per-component provenance. It never mixes vectors from different spaces in one collection or treats one provider as the other's silent fallback. Exact, lexical, and structured work never waits for an unavailable online service.

## 10. Replaceable interface boundaries

The stable seams are intentionally narrow:

| Seam | Receives | Returns | Must not own |
| --- | --- | --- | --- |
| `CaptureDriver` | Source configuration and scoped credentials | Immutable inventory/read handles plus consistency evidence | Content identity, policy, publication |
| `Processor` | Immutable subject/artifact handles, typed parameters, and budgets | Evidence, derived artifacts, representation candidates, or embeddings | Admission, exact identity, annotation authority |
| `RepositoryDriver` | Admitted representation streams and placement policy | Immutable refs, receipts, read/verify/reconcile results | Subjects, views, user tags, logical paths |
| `IndexProvider` | Authorized replayable records and one generation specification | Versioned generation receipt and coverage | Durable facts, identity, query authorization |
| `QueryProvider` | One exact generation ref, typed query, and bounds | Scored `SubjectRef` candidates with provenance | Cross-generation fusion, final authorization |
| Output materializer contract | Frozen export manifest and authorized content handles | Item/final receipts and verification evidence | Live catalog queries, identity, retention policy |
| Later `RetrieverDriver` | Pinned external source and acquisition policy | Candidate bytes and external evidence | Admission or exact identity |

Freedom comes from data contracts, not from granting arbitrary code broad access. Implementations MAY be in-process, isolated local workers, containers, remote services, or protocol adapters if they produce the same typed records and pass the same conformance suite. Core catalog SQL, host paths, signing keys, and repository administration are never extension interfaces.

Provider selection MUST be configuration-driven and capability-based. A plan binds exact provider IDs, profile digests, versions, and parameters. Changing a default affects only new plans or new index generations; it MUST NOT silently reinterpret retained records.

The project deliberately does not add separate privileged `EmbeddingProvider`, `DescriptionProvider`, or `CodecProvider` families. Their algorithms are bounded `Processor` capabilities; only stateful generation storage/query and exact repository placement receive dedicated interfaces:

| User capability | Interface mapping |
| --- | --- |
| Local or online text embedding | `Processor` capability `EMBED_TEXT` |
| Long-form description generation | `Processor` capability `DESCRIBE_SUBJECT` |
| RWKV/Transformer reversible encoding | `Processor` capability `TRANSFORM_EXACT_CANDIDATE` plus `DECODE_REPRESENTATION` |
| Vector persistence and generation lifecycle | `IndexProvider` |
| Vector or hybrid candidate retrieval | generation-specific `QueryProvider`, then the core-owned broker |
| External link acquisition | Later `RetrieverDriver`, always into quarantine |

An `EMBED_TEXT` request contains only immutable segment references, bounded host-issued text handles, language, semantic-profile digest, preprocessing parameters, config digest, authorization/egress scope, and resource budget. Its result contains one vector per accepted segment plus segment ID, element type, dimension, normalization, pooling, model/tokenizer/runtime digests, semantic-space ID, determinism class, coverage, and typed failures. Vector bytes use host-controlled staging or a bounded response; the encoder cannot write zvec or activate a generation directly. The host validates cardinality, finite numeric values, dimension, declared normalization, size, subject binding, and profile compatibility before publishing an index feed.

A `DESCRIBE_SUBJECT` request contains immutable subject/artifact handles, requested description kind/schema, allowed source fields, language, egress policy, profile/config digests, and budgets. Its result is an untrusted candidate document with source spans, citations where supported, coverage, model/runtime provenance, and confidence. The host validates UTF-8, size, schema, provenance, and authorization, then admits a new immutable `DescriptionRevision`; a processor cannot overwrite, accept, or delete a user revision.

An `IndexProvider` receives an authorized replayable feed and one immutable generation specification. It never invokes an embedding model on hidden text. A `QueryProvider` receives one exact generation reference and a bounded typed query; it returns subject/segment candidates and score provenance, never final cross-generation ranking or authorization. Full operation, idempotency, receipt, and failure contracts are defined in [Driver and Processor Interfaces](driver-and-processor-interfaces.md).

## 11. Garbage collection and retention

Path disappearance alone MUST NOT authorize deletion. A representation is eligible for collection only when the core proves it is unreachable from every applicable root and all required migration and verification conditions have passed.

GC roots include:

- committed snapshots and their exact recovery closures;
- retained `ExportManifest` records;
- explicit retention pins and legal or operator holds;
- representations required by a retained decoder or migration closure;
- pending operations and ambiguous external outcomes;
- portable publications whose retention policy remains active.

A `SavedView` is not a root by itself because its membership is dynamic. An operator MAY create a retention policy that periodically freezes its result into a retained manifest or explicit pins.

Indexes, embeddings, thumbnails, and other rebuildable derivatives MAY be collected independently when no retained manifest requires their exact bytes. User-authored annotations MUST NOT disappear because an index generation is deleted.

## 12. Default matrix

| Decision | `RW-MVP-1` default | Optional alternatives |
| --- | --- | --- |
| Exact identity | SHA-256 plus byte length | Additional hashes as evidence only |
| Exact duplicate elimination | Whole-content SHA-256 | Repository-private verified chunk dedup |
| Repository | One qualified lossless driver; engine not yet selected | Other qualified local, NAS, or object drivers |
| Current development repository | In-tree whole-file directory CAS | Not a release default |
| Current personal-use candidate | In-tree `local-zstd-v1`, explicitly selected | Candidate only; not a release default |
| Compression | Qualified lossless repository compression | Qualified exact reversible processor representation |
| Neural/RWKV transform | Disabled | Explicit exact-reversible or approximate representation profile |
| Baseline search | Hybrid lexical + structured + semantic query broker | Acoustic, graph, multimodal, alternate vector spaces |
| Embedding provider | Isolated ONNX Runtime 1.29.x worker with pinned `BAAI/bge-small-zh-v1.5` profile | Another qualified local or remote processor profile |
| Vector store | In-process zvec v0.6.x through `zvec-go`, one local directory per generation | Flat scan, another embedded ANN store, Qdrant, or Milvus later |
| Organization | Tags, annotations, relations, saved views | Original path as provenance/recovery projection |
| Output | Frozen `ExportManifest` materialized on demand | Directory, archive, object/protocol adapter; mounting remains external |
| Source deletion | Disabled | Separately qualified reviewed migration profile |
| Destructive GC | Disabled until root accounting is qualified | Policy-controlled mark and sweep with audit |

## 13. MVP user loop and acceptance

The first qualified user loop is:

~~~text
ingest an explicit source
-> prove exact ContentID and repository placement
-> search or filter through the default fused query broker (with lexical/structured fallback if semantic generation is degraded)
-> add durable tags or notes
-> save a dynamic view
-> freeze an ExportManifest
-> materialize or stream the requested set
-> verify the export or restore exact bytes
~~~

`RW-MVP-1` is not complete until this loop works without requiring users to manually carry internal workspace, root, entry, generation, or repository IDs between ordinary commands. Original-path restore remains a required recovery workflow, but a permanently mounted original tree is not the product's primary interaction or an MVP gate.

## 14. Current status and release blockers

This is the canonical checkout-status matrix. Other plans may add evidence, but they MUST NOT promote an item beyond this table without updating its normative exit gate.

| Capability | Status | Meaning |
| --- | --- | --- |
| Strict persisted configuration and path resolution | implemented and tested | Current schema and host-injected description/index profile bindings are usable; release packaging/profile selection remains open |
| SHA-256 identity and whole-file duplicate placement | implemented and tested | Exact whole-content reuse works in both in-tree profiles |
| `directory-cas-dev-v1` | development only | Raw test driver, never the release default |
| `local-zstd-v1` | candidate | Embedded compression, whole-file dedup, corruption rejection, relocation, profile isolation, explicit repair, copy-forward migration with independently reopened source/target clean readers and rollback preservation, mechanism-separated savings, and non-destructive verified root/inventory candidate planning exist and are tested; encryption admission, chunking, destructive GC, full migration rollback/reader closure, and qualification remain. The separate `local-zstd-encrypted-v1` experimental profile has host-owned AES-256-GCM/key-provider evidence but is not a release default |
| Protection records and digest-bound plan/apply | implemented and tested for the admitted development profile | Exact/link-only/metadata-only decisions and portable recovery references are authenticated; external acquisition remains a later profile |
| Signed recovery publication | implemented and tested for the admitted development profile | Prepared/commit records, signed processor-attempt and portable-fact successor children, portable subject mapping and body attachments, catalog-free exact restore, independent-anchor clean-install import/reader, deterministic recovery-token sets, independently hashed attachment readback, cross-process fencing, and unknown-outcome reconciliation work. Sparse extent maps, broader per-field provenance, production repository qualification, and release acceptance remain open |
| User/imported description revisions and segments | implemented and tested for the admitted recovery profile | SQLite/CLI revisions, source-aligned segments, config/provider/segmentation bindings, and authenticated portable body bindings work; lifecycle and provider admission remain |
| Lexical/structured discovery | implemented and tested for the stated field scope | Required fields, typed filters, segment provenance, coverage, and fused query are wired; semantic remains unavailable when no admitted real bundle is supplied |
| Local ONNX/BGE encoder | implemented and tested for the opt-in worker/component profile | The supervised worker admits the pinned bundle and produces real vectors; the Linux arm64 daemon/CLI semantic E2E passed in the provisioned environment, while packaging and release qualification remain open |
| In-process zvec generation/query | implemented and tested for the opt-in component profile | Real zvec build/open/query evidence and daemon generation-loss/rebuild evidence passed in the opt-in Linux arm64 environment; fixture dimensions do not count as zvec |
| Fused default search | partial | Lexical + structured + real semantic fusion is wired and covered by the executed opt-in Linux arm64 daemon test; profile switching preserves old generations and durable descriptions, while the absence of a supported packaged bundle keeps the default experience honestly degraded |
| Saved views and frozen export manifests | implemented and tested for the stated local scope | `rw view save/get/list/evaluate` and `rw export plan/apply/verify` work end-to-end with exact receipts; release qualification remains |
| Inbox/OpenSubsonic/OPDS | retired historical adapters | Their implementation has been removed; they do not define or advance core completion |
| FUSE/mount/network filesystem | closed non-goal | External tools consume exports or authorized reads |
| OpenList fork/core dependency | closed non-goal | Interoperation may be considered only outside core authority |
| RWKV/Transformer codec | deferred optional profile | Cannot start before simple storage and recovery qualification |

As of 2026-08-22, the admitted Phase 3 development profile has executable recovery closure. Ed25519 `PREPARED_CLOSURE` and `PUBLICATION_COMMIT` records bind payload receipts, authenticated metadata evidence, generation/parent lineage, and a separately retained trust anchor. Signed `PROCESSOR_ATTEMPT_CLOSURE` and complete-state `PORTABLE_FACT_CLOSURE` children bind terminal processor attempts, subject mappings, description and annotation revisions, semantic segments, processor-artifact descriptors, content-addressed bodies, and the admitted typed filesystem facts to the exact parent without changing exact publication identity. The catalog-free reader rejects tampering, missing parents, noncanonical/conflicting successors, unavailable dependencies, dishonest repository verification, and attachment digest or length drift while exact restore remains independent from derived children. The v2 recovery reference and legacy v1 import work without SQLite, indexes, or signing material; the non-destructive v1-to-v2 migration preserves the authenticated publication identity and rejects tampered legacy input. Real daemon/CLI process tests pass on Darwin and Linux arm64. These are implementation tests for development/candidate repositories, not repository, platform, or release qualification.

The recovery closure is further hardened by cross-process publication fencing. Publication now acquires a lease on the `publication_fences` projection through the SQLite store, stamps the returned monotonic fencing token into the signed `PREPARED_CLOSURE`, `PUBLICATION_COMMIT`, processor-attempt, and portable-fact records, validates the lease immediately before every signed-record placement, and releases it on success or failure. An explicit `PublicationFencer` seam keeps the catalog-free reader path (no store) on the legacy `FenceToken: 1` behavior, so existing no-fence tests and readers are unchanged. The fence table plus an opaque per-attempt lease token prevents two processes from interleaving publications in one domain; the fencing token is monotonic across restarts and stale-lease reuse is rejected.

`recovery.import` and `recovery.token.export` are now wired. `recovery.import` verifies a `recovery.export` bundle against an independently retained trust anchor, cross-checks the commit/prepared binding and generation lineage, and admits the closure either catalog-free (clean-install reader over `OpenProfileReadOnly`) or by reconciling the SQLite projection when a store is available. `recovery.token.export` emits the deterministic `org.restoreweave.recovery-token.v1` proof envelope over one subject's recovery reference: schema, snapshot, subject, reference id, expected identity/length, recipe digest, publication commit ref, trust-anchor ref, optional expiry, and a digest over the canonical JSON of all other fields. A metadata-only or explicitly-unprotected subject has no recovery path and fails closed with `ErrNoRecoveryPath`.

Saved views and export manifests are implemented for the stated local scope. `rw view save/get/list/evaluate` manages revisioned dynamic queries (`SavedView`), and `view.evaluate` runs the query through the search engine with the view's structured fields as typed filters. `rw export plan/apply/verify` freezes one view evaluation (or explicit subject set) into an immutable `ExportManifest` with a canonical digest, then materializes exact bytes to an explicit destination via no-follow restore and verifies the destination path-set, lengths, and SHA-256 values against the manifest. `export.apply`/`verify` reference the frozen manifest, never the live view; re-applying the same manifest is idempotent, and unsafe (non-empty or symlink) destinations fail closed. One real daemon/CLI process test now covers configured exact ingest, durable description and annotation writes, typed lexical search with segment provenance, SavedView evaluation, manifest freeze, materialization and verification, recovery-reference/trust-anchor export, catalog/signing-material removal, and clean-install exact restore. This is integration evidence for the current development/candidate profiles, not Phase 6 or release qualification. A mechanism-separated savings report (`MeasureSavings`) reports logical, duplicate, compression, physical, overhead, and net-savings bytes independently for both in-tree profiles.

The lexical/structured search feed now covers the complete baseline fields with typed structured filters, per-description segment provenance, and an honest per-field coverage report. `search.query` accepts `entry_type`, `size`/`mtime` facets, `content_id`, `duplicate_group`, `protection_mode`, `language`, and `suffix` constraints and returns matched segment provenance for description hits. Coverage is measured from an actually built generation; absent fields are reported as absent. Disposable generations bind the resolved config, provider profile, semantic space, snapshot, namespace root, and workspace; mismatches and legacy unbound generations fail closed. The embedding-generation manifest contract binds runtime, model, tokenizer, preprocessing, pooling, normalization, element type, dimension, vector schema, semantic space, distance, index/query configuration, provider, and config digests, and rejects incomplete manifests. With an explicitly admitted local BGE bundle, the daemon now constructs the real semantic binding, publishes and queries zvec generations from durable segments, reports the real capability only after worker/index evidence, and degrades after generation loss until a later rebuild restores it; the opt-in Linux arm64 daemon test for publication, semantic query, generation loss, degraded capability, and rebuild passed in the provisioned environment. This is not release qualification or a packaged default; without the bundle, the semantic dimension reports `SEMANTIC_INDEX_UNAVAILABLE` and fixtures remain non-default.

The portable-fact child is now validated against its parent manifest on the catalog-free read path: every `SUBJECT_MAPPING` record must match the exact parent's raw path, entry type, content identity, logical length, raw name, and protection decision; the record count must cover the manifest entries. Inner records are strictly validated (duplicate logical keys with conflicting bytes, cross-workspace payloads, subject mismatches, and unknown kinds are rejected) before any bundle is admitted.

The admitted Phase 3 file-fact profile captures xattr bytes and the xattr-carried ACL encodings admitted by the current Darwin/Linux adapters when the filesystem exposes them, and otherwise retains explicit `UNOBSERVED`, `UNSUPPORTED`, or `INCONSISTENT` states with reason codes. It authenticates `st_blocks`-based sparse indication but explicitly records the sparse extent map as unsupported; it does not claim hole-by-hole restoration. Broader platform qualification and per-field name/ownership/mode/time provenance require a separately reviewed successor shape. No repository engine is selected; `local-zstd-v1` remains a single-machine measurement candidate.

The current `LINK_ONLY` path is deliberately limited. It hashes readable local source bytes, preserves names/metadata and expected identity, writes no payload to CAS, records locators as `UNVALIDATED`, and blocks full-byte verification and restore before a destination is created. It does not perform external retrieval. A tree default plus exact-path per-file protection overrides are implemented; their effective modes, planned outcomes, reason codes, identities, lengths, and locator bindings are digest-bound. Unresolved readable content is retained exactly as `EXACT_FALLBACK`. Failed or unstable scan entries are retained with requested mode, `BLOCKED` or `UNAVAILABLE` planned outcome, path, scanner state, reason code, and message in a non-executable plan; apply refuses that plan before creating an apply job or mutating the repository.

The narrow operator-resolution path is successor-only. An explicit `METADATA_ONLY` decision may resolve a retained regular-file read/open failure only when a fresh rooted-FD scan proves the same before/after metadata and an included checked boundary. It never resolves an unstable path, lstat/boundary/post-stat/stability failure, directory or symlink failure, special file, path-string capture, cancelled scan, or failed scan. The published namespace retains the raw name, path, metadata, scanner issue, and `EXPLICITLY_UNPROTECTED` outcome without inventing a content identity, file version, representation, or recovery reference. Its scan generation remains `INCOMPLETE` with `full_traversal=false`; authenticated-metadata verification reports the coverage gap and exact restore fails before creating a destination.

Processor capability invocations produce immutable SQLite terminal-attempt rows and a deterministic `restoreweave.processor-attempts/v1` JSON projection linked to admitted artifact references. Success, inapplicability, failure, cancellation, and an unconfigured routed capability are distinguishable. After exact commitment, a signed `PROCESSOR_ATTEMPT_CLOSURE` authenticates that projection and its parent publication without changing exact publication identity. Its v2 complete-state successor chain preserves earlier attempts byte-for-byte, appends later terminal outcomes, and rejects gaps, forks, and historical rewrites. The portable-fact child separately authenticates the corresponding artifact descriptors and content-addressed bodies together with subject mappings and description/annotation records. A reader with the repository and independent trust anchor validates both children without SQLite. The daemon enables only bounded automatic retry of the same signed processor plan, with bound retry intent, idempotency, leases and fencing, unknown-outcome reconciliation, and retry ceilings. Arbitrary manual, rerouted, or general reprocessing remains disabled until a separately reviewed successor contract and release gate admit it.

The remaining durable records use the frozen `PORTABLE_FACT_CLOSURE` shape in
the Restore Manifest contract. It is a signed, complete-state successor chain
over one exact parent. It carries portable subject mappings and immutable fact
descriptors; large description or artifact bodies are SHA-256-addressed
repository attachments rather than SQLite-only bodies or oversized tokens.
This frozen v1 shape is implemented and tested for the admitted development
profile. Catalog-free round-trip, attachment, conflict, clean-import,
corruption, relocation, and reader-dependency tests cover it; expanded fact
profiles require a reviewed successor rather than reinterpretation.

Release blockers follow one dependency graph, not a priority list that optional work may interrupt:

1. The admitted portable authenticated fact and recovery closure is implemented and tested. It includes artifact bodies, portable subject mapping, descriptions/annotations, terminal processor attempts, xattr/ACL observed-or-degraded states, sparse indication, and explicit unsupported extent-map state. Broader per-field provenance or platform profiles require a reviewed successor shape.
2. The admitted clean-install reader/import and independently retained trust-anchor workflow is implemented and tested with real daemon/CLI processes on Darwin and Linux arm64. This does not constitute packaging, independent-failure-domain, or release qualification.
3. Cross-process fencing/lease, typed unknown-outcome reconciliation, and raw/zstd corruption and relocation behavior are implemented and tested for the admitted development profile. Production repository qualification remains Phase 5.
4. After items 1-3, run two gates in parallel: qualify one lossless production repository with complete logical/duplicate/compression/physical/index/model/net-savings measurements; and package the real local ONNX/BGE worker plus zvec while completing lexical/structured fields, typed filters, segment provenance, coverage, model-provider admission, online-egress controls, and host-owned fusion.
5. After both parallel gates pass, complete and qualify saved views, frozen export manifests, materialize/verify, and the ordinary user loop without internal IDs.
6. Run native install, migration, upgrade, backup, performance, and complete `RW-MVP-1` acceptance.

New facades, protocol methods, media applications, mount work, OpenList integration, multimodal dimensions, and neural codecs do not enter this queue. They cannot be used as substitutes for any blocker above.

Fixture embedding processors and named dimensions prove interface shape only. They MUST NOT be reported as a real semantic-search implementation or as completion of this document.
