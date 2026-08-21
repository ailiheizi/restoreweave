# External Source and Retrieval Requirements

> **Profile status:** External reacquisition is a staged `RetrieverDriver` profile above the NAS-first managed-archive MVP. `RW-MVP-1` may expose user-supplied or processor-derived downloadable and reproducible candidates beside baseline local search results, but it preserves readable source bytes through the generic exact route or exact fallback in one qualified mature repository. An external reference is never durable placement, and discovery never authorizes omission or an automatic network operation.

## 1. Purpose

RestoreWeave distinguishes data that is locally authoritative from data that may be reacquired from an external source. A URL, repository revision, package coordinate, model revision, torrent identity, or semantic match is a source candidate, not recovery proof.

This protocol-neutral contract applies to later `RetrieverDriver` implementations for HTTP, Git, software registries, model registries, publisher catalogs, BitTorrent, removable media, and future origins.

## 2. Governing rules

1. Identity is separate from location.
2. Discovering a candidate is separate from binding an immutable source.
3. Binding a source is separate from proving current availability.
4. Provider authenticity is separate from equality to the originally observed source bytes.
5. Technical access is separate from reproduction, transformation, and distribution authority.
6. Retrieved content is hostile until independently verified and released from quarantine.
7. External sources may reduce local storage only after a cold acquisition and objective-specific approval.
8. A source locator never becomes the recovery trust root.

## 3. Identity hierarchy

The system distinguishes:

- `source_candidate_id`: one discovered or user-supplied possibility.
- `source_binding_id`: immutable provider artifact, edition, variant, and expected identity.
- `locator_id`: one mutable or immutable route to bytes or metadata.
- `credential_ref`: access material referenced but not embedded.
- `content_id`: independent digest-and-length identity of acquired plaintext bytes.
- `representation_id`: the qualified recovered or substitute representation.

Multiple locators may resolve one binding. Multiple bindings may produce the same content bytes. A mutable provider name, title, tag, branch, “latest” alias, search result, or magnet display name is not a binding.

## 4. SourceBinding tagged variants

Every binding uses exactly one protocol-specific tagged variant plus common fields.

Common fields include:

- Binding ID, schema version, canonical digest, and creation actor.
- Provider and artifact namespace.
- Immutable revision, edition, locale, platform, architecture, and variant.
- Expected byte length and independent whole-file or object hashes.
- Provider checksums, signatures, transparency evidence, or catalog attestations.
- Required component and dependency closure.
- Original-observation relation and requested recovery outcome.
- Locator references and precedence.
- Credential and entitlement references plus immutable rights-evidence and signed rights-determination IDs, revisions, canonical digests, jurisdictions, and expiry.
- Parser and verifier `Processor` profiles, later `RetrieverDriver` profiles, and host-owned quarantine policy.
- Last metadata validation and last complete cold acquisition.
- Ordered fallback and local-promotion rule.

Protocol-specific fields never share one loose map. Unsupported or missing required fields block qualification.

## 5. Source lifecycle

Required states are:

~~~text
CANDIDATE
-> METADATA_VALIDATED
-> IMMUTABLY_BOUND
-> COLD_ACQUIRED
-> PAYLOAD_VERIFIED
-> COMPONENTS_VALIDATED
-> QUALIFIED
-> PERIODICALLY_REVALIDATED
~~~

Degradation and terminal states include:

- `LOCATOR_UNAVAILABLE`
- `CREDENTIAL_UNAVAILABLE`
- `RIGHTS_BLOCKED`
- `SOURCE_MUTATED`
- `SOURCE_YANKED`
- `WRONG_VARIANT`
- `MISSING_COMPONENT`
- `SIGNATURE_INVALID`
- `HASH_MISMATCH`
- `QUARANTINED`
- `NO_LONGER_REPLAYABLE`

Metadata validation alone never advances directly to `QUALIFIED`.

## 6. Discovery

The MVP baseline catalog search and external discovery are separate operations. A local search query never triggers external network discovery implicitly.

External discovery declares:

- Provider set and exact endpoints.
- Query and candidate budgets.
- Metadata, fingerprint, embedding, title, path, or component data that may leave the workspace.
- Network, privacy, residency, credential, and rights policy.
- Result-retention and purge policy.

Discovery outputs `source_candidate_id` records with provenance and ranking evidence. An LLM, embedding, fingerprint, title, popularity signal, package name, or provider search rank may propose a candidate only. Candidate generation and final acceptance must not be the same learned system without an independent verifier.

RestoreWeave does not ship a default public piracy-index or arbitrary web-search integration.

## 7. Acquisition and quarantine

Every acquisition references:

- An immutable source binding.
- Exact locator and credential revisions.
- Network profile and destination policy.
- Expected hashes, length, components, and signatures.
- Byte, time, cost, retry, redirect, and temporary-space budgets.
- Quarantine root and parser capability policy.
- Current `VERIFIED` signed determinations for reproduction and every other required authority and operation scope, each bound to immutable rights evidence.

Retrieved payloads:

- Land beneath a fresh isolated root.
- Are never executed, installed, mounted, imported, indexed, previewed with privileged decoders, or published automatically.
- Pass protocol-native integrity checks where available.
- Then pass independent RestoreWeave whole-file or object hashing.
- Then pass signature, edition, component, fidelity, malware, and policy checks.
- Remain quarantined when any required result is missing, stale, conflicting, or weaker than requested.

Redirects, mirrors, retries, and multi-source assembly do not change the bound artifact identity.

## 8. HTTPS artifact binding

An HTTPS binding records:

- Original URL and a redacted display form.
- Allowed schemes, origins, hosts, ports, IP ranges, and redirect policy.
- Expected final origin and exact redirect chain when policy pins it.
- Expected length and independent whole-file hashes.
- Detached-signature or publisher-catalog evidence.
- Required HTTP content encoding and representation semantics.
- Range-support and resume policy.
- Authentication and signed-query credential references.

Rules:

- TLS authenticates a connection endpoint; it does not establish immutable payload identity.
- ETag, Last-Modified, filename, Content-Type, and Content-Length are hints unless a provider profile gives them stronger pinned semantics.
- Request identity encoding when byte identity matters and reject transparent content transformation.
- Validate `Range`, `Content-Range`, response status, exact lengths, overlaps, gaps, and reassembled whole-file hash.
- Authentication headers, cookies, passkeys, and signed-query credentials never cross origin or redirect boundaries.
- Resolve and re-authorize DNS and address policy at every connection and redirect.
- Block loopback, link-local, cloud-metadata, private, or other prohibited ranges unless an explicit private-network profile permits them.

## 9. Git binding

A Git binding records:

- Repository identity and canonical remote.
- Object-format algorithm.
- Exact commit object ID and complete reachable object closure required by the recovery contract.
- Signed tag, commit signature, or publisher evidence when required.
- Submodule paths and exact commits.
- Git LFS pointer and object identities.
- Sparse-checkout, partial-clone, filter, shallow-history, and alternate-object-store state.
- Required untracked, ignored, staged, worktree, hooks, configuration, credentials, and local branch or reflog state as separate local observations.
- Optional retained bundle, pack, or source archive and its digest.

A branch, tag name, release title, or hosting-provider URL is mutable unless its immutable object identity is also bound. A clean checkout does not recover uncommitted work, local configuration, credentials, submodules, LFS objects, generated artifacts, or application state unless those components are explicitly protected.

Git hooks and retrieved build scripts are never executed during validation by default.

## 10. Package-registry binding

A package binding records:

- Ecosystem and registry.
- Package namespace and normalized name.
- Exact version and immutable distribution artifact identity.
- Platform, architecture, ABI, runtime, build, feature, and locale variant.
- Registry integrity value and independent whole-file hash.
- Publisher signature or transparency evidence when available.
- Lockfile or solver input and complete transitive dependency closure.
- Yanked, deleted, superseded, deprecation, and license state.
- Credentials, entitlement, and mirror policy.

A version string alone is insufficient when a registry can replace, yank, or serve platform-dependent artifacts. Solver re-execution is not deterministic recovery unless repositories, metadata, resolution rules, and every selected artifact are pinned and cold-tested.

Package scripts, installers, post-install hooks, and binaries remain quarantined and are not executed as source validation.

## 11. Model-registry binding

A model binding records:

- Provider and repository.
- Exact immutable revision.
- Complete required file list with length and digest.
- Weights, shards, indexes, configuration, tokenizer, vocabulary, processor, feature extractor, custom code, generation configuration, and adapters.
- Framework, tensor format, dtype, quantization, architecture, and runtime compatibility.
- Model-card, license, acceptable-use, gated-access, and entitlement records.
- LFS or large-object identities and mirror locations.

Mutable aliases such as `main`, `latest`, or a model name without revision are candidates only. Remote custom code is disabled during acquisition and validation unless a signed allowlisted executable identity and capability sandbox explicitly authorize it.

A quantized, converted, pruned, or merged model is a separate representation and never silently substitutes for the pinned original revision.

## 12. BitTorrent binding

BitTorrent and magnet sources follow [Peer-to-Peer and Magnet Requirements](p2p-and-magnet.md).

The protocol-specific binding preserves v1 and v2 identities, full metainfo or required piece layers, file mapping, selected components, independent whole-file hashes, network profile, and distinct reproduction, distribution, metadata-egress, upload, and public-announcement authority.

Magnet retrieval is one later `RetrieverDriver` implementation. It does not change the general source lifecycle or qualify a source without cold acquisition and independent final verification.

## 13. Application, game, and store-managed content

Collection identity, component roles, capture, restoration, execution boundaries, and phase gates follow [Application and Game Collection Requirements](application-and-game-collections.md).

A store-managed source binding records the provider's stable application, build, depot, package, DLC, branch, platform, locale, architecture, and entitlement identities where available.

The collection resolver separates:

- Reacquirable store-managed payload.
- Saves and user-created content.
- Configuration and preferences.
- Mods, load order, compatibility layers, and plugin state.
- Credentials, activation, device-bound secrets, and cloud-state metadata.
- Signatures, manifests, runtime, and required versions.

The first resolver profile should target one named ecosystem on the selected MVP operating system. A read-only resolver may group files, but it must not execute the application, contact the store, claim application consistency, or mark store data as safely reacquirable without an immutable binding and cold test.

## 14. Reference-only and omission gate

External-source-only treatment is allowed only when:

1. The protection objective explicitly permits `FETCH` for the named components.
2. The immutable source binding and every required dependency are complete.
3. A clean acquisition starts without reusable payload cache and passes independent hashes and component validators.
4. Source, credential, license, decoder, network, immutable rights-evidence, and signed rights-determination dependencies meet freshness policy.
5. Required RPO and RTO are achievable under measured cold conditions.
6. A local exact copy remains through approval and grace.
7. Ordered fallback includes local promotion or an alternative verified placement before source risk exceeds policy.
8. The retained source and verification material is smaller enough to justify the added dependency risk.

One title match, provider listing, live URL, peer observation, metadata fetch, or prior successful download is insufficient.

Phase 0 and Phase 1 do not delete originals or use FETCH as the sole recovery path. Retrievers may create reports, bindings, and cold-test evidence only.

The core records this choice as `LINK_ONLY` with an explicit `LINK_ONLY_UNPROTECTED` or `EXTERNAL_REPLAYABLE` outcome. It retains the original filename, captured metadata, expected identity, immutable binding, and all approved locators, but it does not claim local exact protection. Reacquisition is a separate, user-visible operation that quarantines and independently verifies bytes before creating a local exact representation. A bare URL or mutable locator is never a recovery trust root.

## 15. Revalidation and promotion

Revalidation separately checks:

- Locator resolution.
- Authentication and credential validity.
- Provider metadata and immutable identity.
- Immutable rights-evidence status plus signed rights-determination state, scope, expiry, and revocation.
- Complete cold payload acquisition.
- Independent hash and signature.
- Required component closure.
- Decoder and validator availability.
- Measured RTO, cost, and network policy.

If revalidation fails or expires while the local original remains, the planner promotes exact local protection before omission or retention reduction. If the local original is already gone, health degrades visibly and no unseen substitute is auto-accepted.

## 16. `RetrieverDriver` contract

`RetrieverDriver` capabilities declare:

- Supported source-binding variants.
- Probe, metadata validation, acquisition, and revalidation capabilities separately.
- Network destinations and protocol features.
- Credential requirements, immutable rights evidence, required signed determination types and scopes, and separately scoped distribution approvals.
- Redirect, mirror, resume, multi-source, and cache behavior.
- Hash, signature, transparency, and catalog checks.
- Quarantine and parser dependencies.
- Byte, file, connection, time, cost, and retry limits.
- Determinism and durable evidence outputs.

An entry point enabled for metadata validation is not implicitly enabled for acquisition, upload, announcement, or execution.

## 17. API resources

Required resource families include:

- `/v1/source-candidates`
- `/v1/source-bindings`
- `/v1/source-locators`
- `/v1/retrieval-profiles`
- `/v1/acquisitions`
- `/v1/quarantine-items`
- `/v1/source-revalidations`
- `/v1/source-promotions`

Protocol-specific records are exposed as tagged variants. APIs and agents never receive raw secret-bearing URLs or credentials unless a dedicated secret-read authority explicitly permits it.

## 18. Phase boundaries

### Phase 0

- User-supplied source candidates.
- Offline parsing and immutable-binding proposals.
- No acquisition required for managed-archive MVP success.
- No source-only omission.

### Phase 1

- One constrained HTTPS exact-artifact retriever for report and cold revalidation.
- Optional Git binding and local bundle verification.
- Retrieved content remains supplementary; the exact representation in the qualified repository remains authoritative.
- GitHub release, package-registry, model-registry, and store-specific provider adapters remain experimental or report-only.

### Phase 2

- Qualified provider profiles for Git hosting, package registries, model registries, and one application or game ecosystem.
- Offline magnet inspection and an explicitly approved `DOWNLOAD_ONLY` BitTorrent `RetrieverDriver` profile with technically enforced zero payload-piece and content-derived metadata upload.
- Source-only treatment remains opt-in and requires the full omission gate.

### Phase 3

- Multiple independent retrievers, source routing, trusted encrypted swarm storage, and measured source-diversity planning.

## 19. Acceptance criteria

1. A mutable alias cannot become an immutable binding without a pinned revision and payload identity.
2. Metadata validation cannot be reported as complete cold recoverability.
3. Redirect, mirror, resume, and multi-source acquisition always ends with one independent complete-object hash.
4. Retrieved executables, archives, models, and media remain quarantined until their complete release policy passes.
5. Missing or stale rights evidence or signed determinations, credentials, signatures, components, or validators block source-only treatment.
6. A failed or stale source promotes exact local protection while the original still exists.
7. Phase 1 never deletes an original or treats a `RetrieverDriver` as the sole authoritative path.
8. Local search never triggers external discovery.
9. A provider-specific retriever cannot use undeclared network, credential, execution, upload, or announcement authority.
10. Every qualified source can be reacquired from a clean payload cache within the declared RTO or is visibly degraded.
