# Restore Manifest Requirements

> **Profile status:** This is an extended schema design. The MVP recovery-record minimum and compatibility boundary are defined by [Core Kernel and Interface Requirements](core-kernel-and-interface.md) and [MVP and Operator Contract](mvp-and-operator-contract.md). `RW-MVP-1` is the NAS-first managed-archive profile: it accepts one local or mounted filesystem root through a generic `CaptureDriver` with an explicit consistency claim, preserves readable unknown or processor-failed data through exact fallback, builds a rebuildable baseline metadata and extracted-text index, writes exact admitted bytes to one qualified mature exact repository reported as one placement, and publishes a portable RRF companion closure with a signed `PublicationCommitRecord`. Platform- and engine-specific implementations qualify independently rather than becoming manifest-wide assumptions. Clean recovery begins from a scoped credential source and an independent trust anchor. Capsule Core, ControlPlaneRecoverySet, Recovery Bootstrap Envelope, RecoveryHeadWitness, Recovery Bootstrap Seed, threshold custody, distributed CAS heads, and `COLD_VERIFIED` records below are a superseded enterprise-profile design and are deferred unless a later named profile explicitly activates them.

## 1. Purpose

The restore manifest is the durable, authenticated contract that explains:

- What was observed.
- What must be recovered.
- Which representations, sources, and recipes are available.
- Which validators and approvals apply.
- What claim was requested and achieved.
- How recovery can proceed without the original RestoreWeave control plane.

The manifest must never reduce exact identity, source identity, normalized content, perceptual similarity, function, and semantics to one vague equivalence field.

## 2. Design influences

The manifest combines useful mechanisms from:

- BagIt manifests and fetch records.
- Metalink source sets, priorities, signatures, and piece hashes.
- git-annex content identities and availability audits.
- DataLad and Nix provenance and replayable recipes.
- OCFL logical-path and immutable-version inventories.

These influences provide useful structures but do not replace RestoreWeave's recovery contracts and validator profiles.

## 3. Manifest-level fields

Each manifest must include:

- Schema name and version.
- Snapshot ID, parent snapshot ID, creation time, and host identity.
- Monotonic publication generation, parent publication identity, writer identity, and fencing information.
- Canonical serialization and manifest digest.
- Authentication or digital-signature metadata.
- Policy, pipeline, plugin-registry, and profile-registry digests.
- Source-identity registry, source-transition, and source-journal-checkpoint roots.
- Exact `CaptureSetRecord` ID and canonical digest for the generation.
- Namespace-reconstruction root, RRF root, payload receipt, and prepared-closure receipt.
- Scoped credential-reference metadata without reusable credentials and the independent trust-anchor identity used to authenticate the recovery closure.
- Active legal-hold and anomaly-preservation-hold set digests.
- Retention, immutability, and deletion policy.
- Summary counts and bytes by requested and achieved claim.
- Unknown, unresolved, degraded, and unprotected totals.
- Recovery-time objective and achieved placement count without treating one repository as redundant.
- Restore-test history or authenticated references to test records.

## 4. Logical records and surrounding recovery corpus

The READY_TO_COMMIT manifest may contain or reference separate authenticated records, but it must preserve enough information to resolve every referenced dependency offline.

Required manifest-contained or manifest-referenced logical record types:

- Entity and collection.
- Source identity, signed source-identity transition, and journal checkpoint.
- CaptureSetRecord, one `CaptureRootBindingRecord` per achieved root, provider receipt, and append-only CaptureSetLifecycleEvent records.
- Change-anomaly record and anomaly-preservation hold or release record.
- Asset and filesystem entry.
- Original observation.
- Representation.
- Source binding.
- BitTorrent source binding.
- Derivation or rebuild recipe.
- Recovery contract.
- Validator profile.
- Fact.
- Claim.
- Policy decision.
- Verification run.
- Approval.
- Signed rights-determination records plus distinct `NETWORK_OPERATION`, `SEED_UPLOAD`, and `PUBLIC_ANNOUNCEMENT` operational approval records.
- Storage receipt.
- Namespace-reconstruction entry.
- Physical-placement-ledger entry.
- Compatible reader and repository-format reference.
- Scoped credential-source class and independent trust-anchor reference without secret material.

For `RW-MVP-1`, the surrounding corpus consists of the payload receipt, signed `PREPARED_CLOSURE`, prepared-closure placement receipt, signed `PublicationCommitRecord`, reconciled commit-marker placement evidence, verification records, recovery-reference export, and restore results. The commit record points to the RRF root, payload receipt, and prepared-closure receipt but never contains its own placement receipt. Clean discovery starts only from a valid portable commit marker and ignores an orphan payload or prepared closure. The baseline search index is a rebuildable projection rather than publication authority; its index-generation record binds durable subjects, identification evidence, processor artifacts, coverage, and provenance without becoming necessary for exact recovery.

The staged `SnapshotPublicationRecord`, post-CAS `AtomicPublicationReceipt`, ControlPlaneRecoverySet, Recovery Bootstrap Envelope, RecoveryHeadWitness, Recovery Bootstrap Seed, BootstrapSeedSuccessorRecord, RecoveryArtifactPlacement, placement-checkpoint, witness-transition, fork-resolution, and `COLD_VERIFIED` records described later belong only to the deferred enterprise-profile model. They are not required surrounding records for RW-MVP-1. Later recovery and restore-result records never mutate a signed manifest or RRF root.

## 5. Entry-level minimum fields

Every logical entry must record:

- Stable entry ID.
- Stable asset ID for user-facing identity across path changes.
- Immutable content ID for observed bytes and immutable representation IDs for available recovery graph nodes.
- Root subject ID.
- Entity and component relationships.
- Logical path, entry type, and namespace-reconstruction reference.
- Physical-placement references for available representations.
- Filesystem metadata required by policy.
- Original content length and cryptographic identity when bytes existed.
- Parser coverage, inspected ranges, and errors.
- Recovery contract.
- Available representations, sources, and recipes.
- Dependency and restore-order constraints.
- Decision and approval references.
- Compatible reader and repository-format references required to execute or validate recovery. A later profile may add Capsule Core references.
- Last verification and next required verification.
- Classification and contract history.

## 6. Recovery contract

Illustrative structure:

~~~json
{
  "contract_id": "urn:uuid:...",
  "root_subject_id": "urn:sha256:...",
  "requested_claim": "ORIGINAL_BIT_EXACT",
  "requested_filesystem_restore_claim": "FILESYSTEM_NATIVE_EXACT",
  "acceptable_capture_consistency_levels": [
    "APPLICATION_CONSISTENT_EXPORT",
    "IMMUTABLE_SNAPSHOT",
    "CRASH_CONSISTENT_VIEW"
  ],
  "subject": "ORIGINAL_BYTES",
  "relation": "EXACT",
  "component_requirements": [
    {
      "component_id": "urn:uuid:...",
      "required": true,
      "acceptable_outcomes": ["ORIGINAL_BIT_EXACT"],
      "ordered_fallback_actions": [
        "TRY_NEXT_INDEPENDENCE_GROUP",
        "TRY_STORED_REPLICA",
        "BLOCK"
      ]
    }
  ],
  "validator_profile_refs": ["exact-file@1"],
  "automatic_acceptance": true,
  "unlisted_component_outcomes_allowed": false,
  "omission_approval_ref": null,
  "fallback_policy_ref": "strict-default@1"
}
~~~

Required claim names:

- ORIGINAL_BIT_EXACT
- SOURCE_EQUIVALENT
- NORMALIZED_CONTENT_EQUIVALENT
- PERCEPTUALLY_EQUIVALENT
- FUNCTIONALLY_EQUIVALENT
- SEMANTICALLY_EQUIVALENT

DISCOVERY_MATCH is a candidate relation and cannot satisfy a restore contract.

Each required component must declare a non-empty `acceptable_outcomes` set and explicit ordered fallback actions. A contract must declare `requested_filesystem_restore_claim`. When capture consistency is constrained, it must declare an explicit non-empty set of acceptable capture-consistency levels. Content outcomes, filesystem claims, and capture-consistency levels are independent sets; none is a total ordering or an implicit substitute for another. The manifest must not use a global maximum acceptable claim or assume that claim names form one universal strength ordering.

## 7. Original observation

The original observation anchors later claims:

- Content length.
- SHA-256 digest.
- Optional BLAKE3 and chunk hashes.
- Filesystem metadata.
- Snapshot and observation time.
- Host, mount, source, and snapshot identity.
- Exact `capture_set_id`, canonical capture-set digest, captured-root mapping, and CaptureDriver receipt reference.
- Read coverage, explicit `capture_consistency_level`, and authenticated capture-consistency evidence reference.
- Producer and application context.
- Embedded and sidecar component inventory.

All substitutes and derivatives must retain the root subject ID. They cannot use a previously accepted derivative as the new reference without creating a new explicit original observation.

### 7.1 CaptureRootBindingRecord

A `CaptureRootBindingRecord` is the durable, portable description of the live opaque `CaptureRootBinding` used during capture. It contains:

- Record ID, schema version, canonical digest, capture-set ID, source ID, host ID, and achieved-root ID.
- Filesystem or volume identity, mount identity and properties, configured-root identity, captured root-object identity, and requested-to-achieved root mapping.
- Capture basis: immutable snapshot, export, versioned view, or validated-live identity; provider-native snapshot, dataset, subvolume, export, or barrier identifiers; and applicable lease, hold, deletion-protection, and read-only evidence.
- Resolver profile ID, implementation and version digest, kernel facility profile, symlink policy, magic-link policy, nested-mount policy, special-file policy, and component-relative operation set.
- Validation observations and times for the trusted root anchor, root object, filesystem or volume, mount, capture basis, read-only state, resolver behavior, and permitted read scope.
- Known limitations, unsupported metadata, excluded roots, and the reason the binding may or may not authorize publication.

Runtime file-descriptor numbers, process-local handle values, `/proc/self/fd` paths, and mutable absolute exposure paths MUST NOT appear as durable authority. They MAY appear only in explicitly non-portable diagnostic logs. A clean reader verifies the record and its provider evidence but does not attempt to reopen the original live descriptor.

Every namespace entry and original observation names the exact `CaptureRootBindingRecord` ID and digest that authorized its enumeration or read. If a capture has multiple achieved roots, each entry binds exactly one achieved root; cross-root consistency remains a property of the containing `CaptureSetRecord`.

## 8. Representation record

Every representation must include:

- Representation ID and root subject ID.
- Representation kind: `EXACT_RAW`, `EXACT_REVERSIBLE`, `APPROXIMATE`, or `DERIVED`.
- Recovery-claim reference using the subject, relation, protected-component, validator-profile, and policy-authority vocabulary in Section 6. A discovery-only artifact records its non-recovery disposition instead of inventing a restore claim.
- Lifecycle class: `AUTHORITATIVE_DATA`, `RECOVERABLE_REPRESENTATION`, `REBUILDABLE_DERIVATIVE`, or `EPHEMERAL_CACHE`.
- Producer capability and transformation-purpose metadata, including normalization, perceptual, generative, semantic, or preview intent where applicable.
- Input representation and dependency IDs.
- Plugin, codec, model, tokenizer, dictionary, delta base, runtime, and decoder identities.
- Parameters, precision, and normalization trace.
- Output content digest and length.
- Determinism, streamability, and loss flags.
- Applicable license and access restrictions.
- Repository ownership mode and tagged stored-object and physical-placement references.
- Required validator profiles.
- Creation and verification history.

The three axes answer different questions and must not be collapsed into one role or class. Representation kind describes the stored or derivable form, the recovery claim describes which outcome the representation may satisfy under validation and policy, and lifecycle describes retention and rebuild authority. Source provenance is recorded through the root subject, original observation, and source bindings; rebuildability is recorded through dependencies and lifecycle. Normalized, perceptual, generative, semantic, and preview labels remain transformation-purpose metadata and contribute to a recovery claim only when a qualified validator and policy explicitly permit that claim. A reacquisition recipe remains a source binding until acquired bytes are independently validated, admitted, and placed as a representation.

## 9. Stored-object record

Every stored representation must declare exactly one ownership mode. The record is a tagged union; fields from the two variants must not be mixed implicitly.

~~~json
{
  "representation_id": "urn:uuid:...",
  "content_id": "urn:sha256:...",
  "ownership_mode": "ENGINE_MANAGED_OBJECTS|RESTOREWEAVE_PACKS",
  "engine_managed_objects": null,
  "restoreweave_packs": null,
  "physical_placement_refs": []
}
~~~

### 9.1 ENGINE_MANAGED_OBJECTS

The external engine owns chunking, deduplication, compression, encryption, packs, physical object names, and implementation-private object IDs. RestoreWeave records:

- Stable engine storage-binding ID.
- Engine, adapter, reader, repository-format, and configuration-schema identities and an immutable digest of format-affecting, location-independent configuration.
- Logical-path or selection semantics needed to interpret an engine restore point without embedding any particular mutable restore-point handle.
- Required Capsule Core reader, configuration-schema, and historical format-reader references.
- One or more `physical_placement_refs` that resolve the current or historical repository instance, account, engine restore-point handle, receipts, fencing, retention, and verification state through the placement ledger.

Engine-internal chunk, pack, or object IDs are optional diagnostic data and must never become permanent RestoreWeave content or representation identity.

Every engine restore point created from a captured source binds the exact `CaptureSetRecord` ID and canonical digest in the engine request, authenticated engine receipt, independent verification, and placement-ledger creation event. Multiple repository restore points may satisfy one `CRASH_CONSISTENT_VIEW` generation only when all of them bind the same capture-set digest.

### 9.2 Deferred RESTOREWEAVE_PACKS

RW-MVP-1 does not implement or qualify this ownership mode. The schema is retained for a possible later native-storage profile.

RestoreWeave owns plaintext chunk identities, compression, immutable pack assembly, authenticated encryption, pack-object identity, and immutable repair-coding semantics. RestoreWeave records:

- Plaintext content and chunk identities, logical and encoded lengths, codec envelope, and compression parameters.
- Immutable pack-object ID, authenticated-encryption envelope, encoded-object digest, and authenticated index or footer digest.
- Immutable redundancy or erasure-coding scheme and shard identities when they are part of the representation itself.
- One or more `physical_placement_refs` that resolve backend locators, repository instances, accounts, regions, receipts, readback or scrub evidence, repair health, retention, and placement state through the placement ledger.

For both modes, the stored-object record is immutable and binds the tagged record to the exact representation ID. Backend movement, account changes, new receipts, readback results, retention transitions, and health changes append placement-ledger generations rather than rewriting the signed stored-object record. Verification records distinguish detectable corruption from repairable corruption.

## 10. Source binding

Each source candidate must include:

- URI or provider-native ID.
- Provider, protocol, resolver, and resolver digest.
- Immutable version, release, edition, locale, platform, architecture, region, and variant.
- Expected content length and artifact digest.
- Publisher signature or signed catalog identity when available.
- Priority and failure-independence group.
- Authentication and entitlement requirements without raw secrets.
- License, expiry, and access restrictions.
- Redirect and canonicalization policy.
- Last successful full acquisition and verification.
- Restore-time and rate-limit observations.
- Relationship to the original.

Allowed relationship values:

- EXACT_MATCH_OBSERVED
- DERIVED_FROM_SOURCE
- USER_ACCEPTED_REPLACEMENT
- UNKNOWN

UNKNOWN cannot automatically justify deletion of the original.

### 10.1 BitTorrent source binding

A BitTorrent binding is a specialized source binding. A magnet URI is an identity and discovery hint, not a complete recovery record. Reference-only treatment for critical content should retain authenticated metainfo because peer metadata exchange may become unavailable.

Illustrative structure:

~~~json
{
  "binding_type": "BITTORRENT",
  "binding_id": "urn:uuid:...",
  "root_subject_id": "urn:sha256:...",
  "asset_id": "urn:uuid:...",
  "expected_content_id": "urn:sha256:...",
  "torrent_format": "V1|V2|HYBRID",
  "magnet": {
    "redacted_uri": "magnet:?xt=...&tr=https%3A%2F%2Ftracker.example%2F<redacted>",
    "raw_uri_secret_ref": "secret-object:...",
    "exact_topics": [
      {
        "kind": "BTIH_V1",
        "source_encoding": "hex|base32",
        "digest_algorithm": "sha1",
        "digest_hex": "..."
      },
      {
        "kind": "BTMH_V2",
        "multihash_hex": "1220...",
        "digest_algorithm": "sha2-256",
        "digest_hex": "..."
      }
    ],
    "display_name": "advisory only",
    "tracker_uris": [],
    "ws_web_seed_uris": [],
    "dht_locator_hints": [],
    "direct_peer_hints": [],
    "select_only_raw": "0,2,4-6",
    "unknown_parameters": []
  },
  "metainfo": {
    "metainfo_kind": "HYBRID",
    "retention_mode": "RETAINED",
    "retained": {
      "storage_mode": "OBJECT_REFERENCE",
      "torrent_file_state": "RETAINED",
      "torrent_object_ref": "...",
      "torrent_file_sha256": "...",
      "raw_info_object_ref": "...",
      "raw_info_length": 0,
      "raw_info_sha256": "...",
      "piece_layers": {
        "state": "RETAINED",
        "object_ref": "...",
        "sha256": "..."
      }
    },
    "reacquire_only": null,
    "v1_infohash_sha1": "...",
    "v2_infohash_sha256": "...",
    "authenticity": {
      "state": "PUBLISHER_AUTHENTICATED|CAPTURE_AUTHENTICATED|INTEGRITY_ONLY",
      "attestation_refs": []
    },
    "resource_limits": {
      "max_bep9_info_bytes": 16777216,
      "max_full_metainfo_bytes": 67108864,
      "max_v2_piece_layers_bytes": 50331648
    }
  },
  "discovery": {
    "private": false,
    "tracker_tiers": [],
    "dht_allowed": true,
    "bootstrap_nodes": [],
    "web_seeds": [],
    "direct_peer_hints": []
  },
  "mutable_source": {
    "kind": "BEP46",
    "xs_urn_btpk": "urn:btpk:...",
    "public_key_hex": "...",
    "salt_hex": null,
    "dht_target_hex": "...",
    "accepted_sequence": 0,
    "rollback_floor_sequence": 0,
    "signature_hex": "...",
    "signature_verified": true,
    "raw_signed_item_object_ref": "...",
    "raw_signed_item_sha256": "...",
    "last_resolved_v1_infohash_sha1": "...",
    "last_resolved_at": "...",
    "frozen_binding_ref": "..."
  },
  "selected_files": [
    {
      "asset_id": "urn:uuid:...",
      "expected_content_id": "urn:sha256:...",
      "root_subject_id": "urn:sha256:...",
      "namespace_reconstruction_ref": {
        "record_set_id": "...",
        "generation": 42,
        "entry_key": "...",
        "record_set_digest": "sha256:..."
      },
      "torrent_file_index": 0,
      "raw_path_components_base64": [],
      "sanitized_display_path": "...",
      "restore_path": "...",
      "length": 0,
      "attributes": {
        "padding": false,
        "symlink": false,
        "executable": false,
        "hidden": false
      },
      "v1": {
        "concatenated_offset": 0,
        "first_piece": 0,
        "last_piece": 0,
        "boundary_piece_dependencies": [],
        "padding_dependencies": []
      },
      "v2": {
        "piece_length": 0,
        "pieces_root_sha256": "...",
        "piece_layer_state": "NOT_REQUIRED",
        "piece_layer_ref": null
      },
      "original_identity": {
        "length": 0,
        "sha256": "..."
      }
    }
  ],
  "rights_determination_refs": {
    "user_reproduction_authority": {
      "rights_determination_id": "...",
      "resource_revision": 5,
      "canonical_digest": "sha256:..."
    },
    "source_distribution_authority": {
      "rights_determination_id": "...",
      "resource_revision": 8,
      "canonical_digest": "sha256:..."
    }
  },
  "network_operation_approval_ref": {
    "approval_id": "...",
    "resource_revision": 3,
    "canonical_digest": "sha256:..."
  },
  "seed_upload_approval_ref": null,
  "public_announcement_approval_ref": null,
  "acquisition_evidence_refs": [],
  "fallback_policy_ref": "torrent-strict@1"
}
~~~

Required BitTorrent rules:

- Preserve a report-safe redacted magnet URI and parsed repeated parameters. An exact raw URI may be retained only in a separately encrypted, access-controlled secret object referenced by `raw_uri_secret_ref`.
- Private-tracker passkeys, tokens, embedded credentials, and credential-like unknown parameters must never appear in normal signed/report-visible manifest, log, or API fields. Preserve non-secret unknown parameters; classify uncertain parameters as secret and fail closed.
- Tracker (`tr`), web-seed (`ws` or metainfo web-seed), DHT bootstrap or locator, direct-peer, and mutable-source fields are separate untrusted locator records. None contributes to torrent or payload identity.
- Normalize v1 `btih` identifiers to the full 20-byte SHA-1 value and v2 `btmh` identifiers to the decoded full multihash and full 32-byte SHA-256 digest.
- A hybrid binding must retain and verify both exact topics when both are known.
- Display name, tracker URL, search result, and file name are discovery evidence only.
- Strictly validate bencoding and calculate each infohash over the raw encoded `info` dictionary. The raw bytes or an authenticated object containing them must be retained when policy requires deterministic offline verification.
- The full v2 infohash must be retained. A 20-byte truncated tracker, DHT, or wire identifier is discovery evidence and cannot satisfy metainfo identity.
- A BitTorrent infohash proves integrity binding to metainfo; it does not by itself authenticate the publisher, rights, safety, or user intent. Publisher and RestoreWeave capture attestations must be represented separately.
- `metainfo` is a strict tagged union. `retention_mode=RETAINED` requires non-null `retained`, null `reacquire_only`, an authenticated raw-info object reference and digest, and `storage_mode=EMBEDDED|OBJECT_REFERENCE`. Its nested `torrent_file_state` is `RETAINED` with non-null full-torrent object reference and digest, or `NOT_CAPTURED` with both null; this permits exact BEP 9 information-dictionary retention when no original root metainfo bytes were obtained. `retention_mode=REACQUIRE_ONLY` requires null `retained` and a non-null `reacquire_only` record containing an exact omission-approval reference, acquisition-profile digest, last successful cold-drill reference and time, expected metadata identities, whether required v2 piece layers must be reacquired, and next drill deadline. Fields from the variants must not be mixed.
- Every retained torrent metainfo, raw-info, and piece-layer reference must resolve to an authenticated stored-object record covered by the signed manifest. `INTEGRITY_ONLY` means publisher authenticity is unknown; it does not permit unauthenticated local metadata.
- The binding must set positive policy limits before parsing or network acquisition. Numeric values in the illustrative structure are examples, not universal defaults. `max_bep9_info_bytes` bounds only the BEP 9 raw `info` dictionary; `max_full_metainfo_bytes` separately bounds complete `.torrent` bytes; `max_v2_piece_layers_bytes` separately bounds the root-level v2 piece-layer dictionary. One limit must not be reused as an implicit allowance for another class.
- The retained variant's `piece_layers.state` is exactly one of `NOT_APPLICABLE_V1`, `NOT_REQUIRED`, or `RETAINED`. The first two require null object reference and digest. `RETAINED` requires both. Any retained v2 or hybrid metainfo with at least one non-empty file larger than `piece length` must use `RETAINED`; a v2 or hybrid file no larger than one piece does not itself require a layer. Selected files independently record `piece_layer_state=REQUIRED|NOT_REQUIRED` and a conditional file-level reference. Piece-layer hashes must be checked against each file's `pieces root`.
- `REACQUIRE_ONLY` metainfo nulls every retained-object reference and cannot satisfy a declared offline recovery path. It requires explicit omission approval and a successful cold metadata-acquisition drill that reacquires and verifies all required piece layers; otherwise the entry remains `BACKUP_RECOMMENDED`.
- Tracker tiers must preserve tier structure. Web seeds, DHT bootstrap nodes or locators, direct peers, tracker scrape counts, and reported seed counts are availability observations, not replicas.
- If `private=1` is present, the resolver must follow BEP 27 strictly: it may announce only to the private tracker currently selected and may initiate connections only to peers returned by that tracker. DHT, PEX, local discovery, magnet `x.pe`, public direct-peer hints, and other peer sources are disabled. With multiple private trackers, only one is used at a time; switching occurs only after failure, disconnects all existing peers, and then connects only to peers returned by the newly selected tracker. Private tracker credentials and passkeys are secret references, never plaintext report fields.
- A BEP 46 mutable source is a signed moving locator, not immutable torrent identity. Before freezing a resolution, the resolver derives and verifies the BEP 44 target from the public key and optional salt, validates the signature over the exact canonical signed bytes, retains authenticated raw signed-item evidence, and enforces a monotonic sequence floor for each key-and-salt identity. Rollback, same-sequence equivocation, invalid signatures, and target mismatch are rejected or quarantined. Every accepted resolution creates or references a new immutable frozen source binding; an update must never rewrite a previously signed binding. Loss of DHT republishing or observation of a higher sequence produces source degradation, not automatic snapshot mutation. BEP 46 directly yields only a 20-byte v1 infohash, but subsequently authenticated hybrid metainfo may freeze both its verified v1 identity and full v2 SHA-256 identity.
- `NETWORK_OPERATION` approval alone never authorizes upload or public discovery. Payload upload or seeding requires applicable redistribution rights and a separate current `SEED_UPLOAD` approval reference. Public tracker/DHT announcement or public swarm publication requires applicable public-announcement or disclosure rights, explicit public-announce and content-derived-egress switches in `NETWORK_OPERATION`, and a separate current `PUBLIC_ANNOUNCEMENT` approval reference. All three approval references bind an exact approval ID, resource revision, and canonical digest; they are independently revocable and absent by default.

Each selected file must record:

- Stable asset ID, expected content ID, root subject ID, and namespace-reconstruction reference.
- Torrent-native file index when defined, raw path components, sanitized display path, and mapped restore path.
- Length, selected state, and file attributes including padding, symlink, executable, and hidden markers where present.
- For v1: concatenated-stream offset, first and last piece, boundary-piece dependencies, and padding dependencies.
- For v2: piece length, full SHA-256 `pieces root` for non-empty files, the computed `piece_layer_state`, and a conditional piece-layer reference.
- For hybrid: validated agreement of v1 and v2 names, order, lengths, padding, and alignment.
- Independent original whole-file length and SHA-256. Torrent-native hashes do not replace this identity for `ORIGINAL_BIT_EXACT`.

Acquisition evidence must include:

- Binding, exact-topic, metainfo, selected-file-map, user-reproduction-authority, source-distribution-authority, and the exact revision and canonical digest of every applicable `NETWORK_OPERATION`, `SEED_UPLOAD`, or `PUBLIC_ANNOUNCEMENT` approval.
- Resolver/client, build, runtime, and environment identities.
- Start and completion time, network context, discovery methods, tracker tiers attempted, DHT use, web seeds, and peer-source counts.
- Whether complete metadata was acquired and verified before paths or selection were trusted.
- Required-piece coverage, piece or Merkle verification results, boundary-piece handling, and incomplete-piece map.
- Final assembled length and independent whole-file SHA-256 for every selected file.
- Evidence-log digest, cold-restore status, elapsed time, and final retrieval outcome.

BitTorrent verification requirements:

- V1 pieces must match the metainfo SHA-1 piece hashes. Because v1 uses SHA-1 and has no mandatory modern per-file whole hash, final success additionally requires the original whole-file SHA-256.
- V2 blocks and pieces must verify through SHA-256 Merkle proofs to each file's `pieces root`; completion must recompute the root and length. Original-bit-exact success additionally requires the independent original whole-file SHA-256.
- Hybrid acquisition must validate mapping consistency and, while using both swarms, verify both hash formats. Any format fallback or disagreement must be recorded and cannot relax final whole-file verification.
- Magnet `so` selection is advisory until metainfo identity is verified and the indices are resolved into the authenticated selected-file map.
- Paths, symlinks, padding, duplicate destinations, invalid encodings, and platform-normalization collisions must be validated before materialization.

## 11. Recipe record

A retrieval, transform, decode, or rebuild recipe must record:

- Recipe type, format, and version.
- Declarative non-shell step graph, or a signed allowlisted executable identity with immutable digest and signature.
- Structured argument vector, typed inputs and outputs, working-directory policy, and no ambient shell interpolation.
- Exact toolchain, runtime, container, and environment identities.
- Ordered inputs and their digests.
- Model, dictionary, base object, and decoder dependencies.
- Network dependencies and whether they are retained.
- Named configuration and secret references.
- Expected output identity and the explicit component-scoped acceptable outcomes inherited from the bound recovery contract.
- Required validator profile.
- Last clean execution result.

An arbitrary command string, prompt-produced shell fragment, script body without a signed identity, or mutable executable path is not executable authority. Every executable step runs under a mandatory capability sandbox that explicitly grants only required filesystem roots, network destinations, credentials, devices, subprocesses, resources, and time. Unlisted capabilities are denied, and the granted capability-policy digest is part of the recipe and verification evidence.

A recipe never tested in a clean environment is unverified.

## 12. Facts, claims, decisions, and proofs

### Fact

Examples:

- SHA-256 equals a recorded digest.
- libmagic reports a MIME type.
- VMAF produced a particular score.
- A signature validation passed.
- A parser inspected 100 percent of required content.

### Claim

A claim references facts and states a relation between a root subject and candidate. It must include limitations and expiry.

### Decision

A decision records which policy selected storage, source-only, rebuild, transform, or review treatment.

### Verification run

A verification run must be replayable:

~~~json
{
  "run_id": "urn:uuid:...",
  "profile_id": "audio-perceptual-full-track@1",
  "profile_digest": "sha256:...",
  "observed_at": "2026-08-10T00:00:00Z",
  "runner_identity": "...",
  "environment_digest": "sha256:...",
  "reference_input_digests": [],
  "candidate_input_digests": [],
  "normalization_trace": [],
  "metric_results": [],
  "outcome": "PASS",
  "evidence_log_digest": "sha256:..."
}
~~~

Required outcomes:

- PASS
- REVIEW
- FAIL
- INCONCLUSIVE

### 12.1 Identification, parsing, extraction, and model evidence

When applicable, the recovery corpus contains authenticated records defined by [File Identification and Extraction Requirements](file-identification-and-extraction.md):

- `DetectionEvidenceRecord`
- `ParseCoverageRecord`
- `VirtualMemberRecord`
- `ExtractionResult`
- `FingerprintRecord`
- `EmbeddingRecord`
- `EvaluationCorpusRecord`

Each record binds the exact observation or component, entry-point and package digest, configuration and dependency digests, execution reproducibility class, inspected ranges, coverage and truncation, output digest, sensitivity and ACL lineage, and purge or rebuild lineage. Seed, sampler, runtime, device, driver, precision, and numerical mode are required when they can affect output.

When processing was remote, the durable result may also bind the applicable `RemoteProcessorEgressProfile` digest and opaque Processor provenance. Provider accounts, prompts, model routing, token accounting, and training terms remain Processor or external-harness concerns rather than manifest policy fields.

Ambiguous, polyglot, conflicting, partial, encrypted, malformed, unsupported, and unknown evidence remains explicit. Discovery, fingerprint, embedding, caption, summary, learned-classification, and LLM-output records are never serialized as exact verification or omission authority.

### 12.2 ProtectionObjective and effective policy

Every protected component references one immutable `ProtectionObjective` and its `EffectivePolicyRecord` from [Protection Policy and Planning Requirements](protection-policy-and-planning.md). The binding includes objective revision and canonical digest; RPO, RTO, retention and tombstone rules; restore priority; replica and independence requirements; immutability; drill cadence; offline closure; budget; privacy, residency, network, and rights constraints; selector membership digest; contributing policy versions; field-level provenance; conflicts; approvals; and compiler identity.

An unresolved policy conflict, missing objective, or mutable unversioned default blocks omission, retention reduction, and publication of a weaker protection claim.

## 13. Validator profiles

Every profile must be embedded or referenced by immutable digest and include:

- Profile ID and version.
- Claim, subject, and component scope.
- Implementation, package, model, and runtime digests.
- Input contract and required coverage.
- Canonicalization, alignment, and ignored-field rules.
- Metric definitions.
- Pass, review, fail, and inconclusive rules.
- Calibration dataset and code digests.
- Reference-material class.
- Privacy and resource requirements.
- Fail-closed behavior.

Each metric declares:

- Name, implementation, model, units, and direction.
- Pass threshold and review band.
- Hard-fail threshold.
- Required or advisory role.
- Segment, region, percentile, and aggregation method.
- Missing-data behavior.

### 13.1 exact-file profile

- Original length and SHA-256 equality are required.
- Optional BLAKE3 and chunk hashes may improve speed and repair but do not relax SHA-256 equality.

### 13.2 source-equivalent profile

- Provider artifact identity and digest are required.
- Same label with a changed digest is source drift.
- Original-bit-exact requires an additional original digest match.

### 13.3 normalized-content profile

- Canonical decoder and normalizer are pinned.
- Every required component and time or spatial mapping is covered.
- Ignored container and metadata fields are explicit.

### 13.4 perceptual profile

- Structural gates and at least one calibrated perceptual metric are required.
- Full track, frame, page, region, or equivalent coverage is declared.
- A single uncalibrated score cannot auto-pass.
- Perceptual evidence never produces exact success.

### 13.5 functional profile

- Test harness, environment, inputs, seeds, fixtures, and repeated-run policy are pinned.
- All required tests pass.

### 13.6 semantic profile

- Parser, schema, invariant set, ordering, units, locale, and tolerance rules are pinned.
- LLM output may be advisory but cannot be the sole auto-pass validator.

### 13.7 Capture consistency and filesystem restore claims

Content claims, capture consistency, and filesystem restore fidelity are three orthogonal axes. A bit-exact content result does not imply a valid application transaction boundary or native filesystem fidelity.

Every protected entity must record exactly one `capture_consistency_level` from the qualified `CaptureDriver` vocabulary:

- IMMUTABLE_SNAPSHOT
- APPLICATION_CONSISTENT_EXPORT
- CRASH_CONSISTENT_VIEW
- VERSIONED_OBJECT_VIEW
- BEST_EFFORT_LIVE_VIEW
- CONSISTENCY_UNVERIFIED

The capture-consistency record binds the immutable `CaptureSetRecord`, every applicable `CaptureRootBindingRecord`, CaptureDriver package and entry-point digests, driver receipt, source filesystem or volume identities, requested roots and captured-root mapping, generation, snapshot or barrier identity, freeze or export procedure, start/barrier/ready times, declared lease deadline, cross-volume atomicity and skew, application/plugin/schema/storage-engine versions, transaction or log-sequence identifiers, required logs and journals, and recovery-validation evidence as applicable. Separate append-only `CaptureSetLifecycleEvent` references preserve consumer binding, completion or abandonment, lease changes, release, and cleanup evidence without mutating the CaptureSetRecord. File hashes alone cannot raise the capture level.

A `CaptureSetRecord` is valid for authoritative use only while its canonical digest, provider receipt, source identity, every root-binding digest, scoped read exposure, declared consistency evidence, current lifecycle-event chain, applicable lease or hold, and fencing evidence validate. Publication additionally requires evidence that traversal remained component-relative to the retained root anchor, boundary validation was complete, the root, mount, and snapshot or validated-live basis were not substituted, and special-file policy was enforced before content opens. A scanner result labeled complete cannot satisfy this gate by itself. A driver that falls back from a stronger requested boundary to a changing live tree must create a distinct record with the weaker achieved class; it cannot reuse an immutable-snapshot or crash-consistent identity. Under `RW-MVP-1`, the inventory root, admitted exact-content records, one qualified repository payload receipt, RRF root, prepared closure, and portable publication commit must name the same platform-neutral capture-set digest and applicable root-binding digests.

Every restore contract must independently declare one `requested_filesystem_restore_claim`, and every restore result must separately record `achieved_filesystem_restore_claim`:

- FILESYSTEM_NATIVE_EXACT
- PORTABLE_LOGICAL_EQUIVALENT
- CONTENT_BYTES_ONLY

The filesystem claim binds the required metadata set, destination capability probe, raw path handling, ownership mapping, ACL/xattr/alternate-stream/resource-fork coverage, hard-link and symlink semantics, sparse extents, flags, timestamps, collision handling, degradation action, validator profile, and evidence log.

Neither enum is an acceptable content outcome, and neither may be inferred from `ORIGINAL_BIT_EXACT` or any other content claim. Restore results report all three axes separately.

## 14. Authority and approval records

### Deferred KeyRecoveryPolicy and ceremony evidence

RW-MVP-1 records only the scoped credential-source class and independent trust-anchor reference needed for clean recovery; it never embeds a reusable credential, and a key supplied only by the companion being authenticated cannot satisfy independent trust. The threshold-custody model below applies only to a later named profile.

A signed immutable `KeyRecoveryPolicy` binds every key and credential required by a declared offline path, including encryption and signing roots, recovery-authority roots, Recovery Bootstrap Seed trust anchors, each qualified repository credential or key bootstrap, and required backend account-recovery dependencies. It records policy revision and digest, protected purposes and dependents, recipients and custodians, threshold and independence groups, share or wrapped-recipient identifiers, permitted recovery environments, out-of-band anchor references, refresh and test cadence, expiry, successor policy, and compromise or quorum-loss behavior.

A policy never embeds raw shares, passwords, private keys, recovery codes, or reconstructed secrets. A signed `KeyRecoveryCeremonyRecord` names the policy revision, ceremony type, participating custodian and share IDs, authenticated environment, threshold outcome, recovered-key fingerprint or credential-validation result, repositories and generations tested, cleanup or zeroization evidence, times, exceptions, and final state without revealing secret material.

Lifecycle evidence distinguishes creation, distribution, acknowledgement, successful or failed testing, lost share, compromised share, custodian replacement, share refresh, supersession, and revocation. A new share generation or successor policy is append-only. Missing threshold, stale ceremony evidence, or unrecoverable repository credentials blocks complete offline-recovery health even when ciphertext is intact.

### Omission approval

Authorizes removal or non-retention of a stronger representation before the original disappears.

### Restore acceptance

Authorizes a particular non-exact or review-band candidate during recovery.

### FRESHNESS_RECOVERY_EXCEPTION approval

Authorizes only the explicitly named recovery action when global head freshness cannot be established. By default it may permit non-destructive extraction to a new empty destination; it never implies overwrite, publication-pointer advancement, branch selection, omission, retention reduction, placement retirement, or deletion. It binds the complete observed witness set, persisted highest-seen state, unavailable witness identities, exact snapshot and component scope, destination identity and preflight digest, allowed operations, expiry, reason, and recovery-authority quorum. Any destructive or lifecycle action requires freshness resolution plus its own action-specific approval.

Illustrative signed payload:

~~~json
{
  "approval_type": "FRESHNESS_RECOVERY_EXCEPTION",
  "approval_id": "urn:uuid:...",
  "resource_revision": 1,
  "publication_domain_id": "...",
  "publication_record_digest": "sha256:...",
  "requested_selection_digest": "sha256:...",
  "observed_witness_set_digest": "sha256:...",
  "highest_seen_state_digest": null,
  "highest_seen_state_absence_reason": "NO_TAMPER_EVIDENT_STATE_AVAILABLE",
  "unavailable_witness_ids": [],
  "destination": {
    "destination_id": "...",
    "preflight_digest": "sha256:...",
    "must_be_new_and_empty": true
  },
  "allowed_operations": [
    "READ_ONLY_INSPECTION",
    "NON_DESTRUCTIVE_EXTRACT_TO_NEW_DESTINATION"
  ],
  "not_before": "...",
  "expires_at": "...",
  "reason": "...",
  "recovery_authority_policy_digest": "sha256:...",
  "canonical_digest": "sha256:...",
  "signatures": []
}
~~~

Exactly one of a non-null `highest_seen_state_digest` or a non-null `highest_seen_state_absence_reason` is required. The absence reason is closed to `NO_TAMPER_EVIDENT_STATE_AVAILABLE`, `STATE_PRESENT_BUT_UNREADABLE`, or `STATE_AUTHENTICATION_FAILED`; unreadable or unauthenticated bytes and their digest remain evidence and are included in the observed evidence set. The allowed-operation set is closed to the two non-destructive values shown above. The destination preflight must be revalidated immediately before materialization. The approval requires the threshold recovery authority defined by `recovery_authority_policy_digest`; an ordinary writer, operator, restore approver, storage credential, delete credential, or LLM cannot issue it. It neither changes the authoritative head nor selects a fork.

### Rights determination

A signed rights determination records whether the cited legal, license, entitlement, or organizational basis establishes the named authority for the specified operation scope. It is policy input only and does not execute or operationally approve any action. It must record:

- Authority type: `USER_REPRODUCTION_AUTHORITY` or `SOURCE_DISTRIBUTION_AUTHORITY`.
- Authority state: `VERIFIED`, `EXPIRED`, `REVOKED`, `CONFLICTING`, or `UNKNOWN`.
- Rights holder, provider, license, entitlement, purchase, or organizational-policy basis.
- Allowed operations, including download, decrypt, transform, retain, export, peer upload, seeding, public announcement, and public disclosure of content or swarm identity.
- Territory, account, device, user, purpose, and redistribution restrictions.
- Effective time, expiry, revocation, and required re-authorization events.
- Evidence or contract references without embedding confidential documents unnecessarily.

The signed rights determination is evidence and policy authority, not an operational approval. It does not authorize network access and cannot be created or replaced by an operational approval or break-glass issuance mode.

`USER_REPRODUCTION_AUTHORITY` records why the user may reacquire, reproduce, decrypt, retain, or transform the work. `SOURCE_DISTRIBUTION_AUTHORITY` records why the selected provider, publisher, tracker-controlled source, or other source is authorized to distribute the artifact. They are independent signed determinations, each referencing exact immutable rights evidence.

Possession, a matching digest, a purchase receipt without applicable terms, or successful technical access is not legal authorization. `UNKNOWN` authority for either required determination blocks reference-only omission while original bytes still exist. Peer upload or seeding additionally requires an explicit allowed redistribution operation in the applicable current signed rights determination.

### Legal hold record

A legal hold is a signed immutable authority record, not a mutable flag. It binds hold ID, exact scope and selector membership digest, authority, jurisdiction, reason, evidence references, creation and review times, expiry or indefinite state, affected retention and deletion operations, and release-authority policy. Release is a separately signed event. A current hold overrides retention, garbage collection, source omission, placement retirement, privacy deletion, and crypto-erasure for its scope.

Privacy-deletion and erasure results distinguish catalog purge, derivative and index purge, restore suppression, credential removal, crypto-erasure, backend deletion, blinded or tombstoned immutable evidence, legal-hold retention, and externally unrecoverable copies. They never claim recall from public swarms or third parties without verifiable evidence.

### Anomaly preservation hold

A signed system `ANOMALY_PRESERVATION_HOLD` is an immutable safety record created from one or more deterministic `ChangeAnomalyRecord` digests. It binds source identity, compared complete scan generations, detector and configuration digest, severity and reason codes, affected selector membership, pre-event baseline, anomaly window, protected publication generations, creation time, and policy epoch.

The hold does not assert malware or ransomware certainty and never blocks capture of new exact bytes. While active it blocks source-only promotion, omission, retention reduction, placement retirement, and garbage collection for its scope. A release is a separately signed `AnomalyHoldResolutionRecord` binding the evidence reviewed, actor, reason, retained recovery point, follow-up complete scan or drill evidence, and trusted time. Neither an ordinary approval nor an LLM output can clear it.

### APPLICATION_DYNAMIC_EXECUTION approval

Authorizes only a named application or game dynamic-validation run in an approved disposable environment. It binds the exact collection and collection-revision identities, executable and dependency digests, functional validator profile, sandbox and rollback profile, allowed child processes, devices, filesystem roots, environment variables, temporary credential references, resource and time limits, expected side effects, output-capture policy, and either a deny-all network policy or an exact network-profile revision.

This approval does not authorize installation or execution on the canonical restore destination, release from quarantine, source acquisition, omission, weaker fidelity, public network access, DRM bypass, privileged drivers, or policy publication. Expiry, revocation, collection drift, executable drift, validator change, sandbox change, credential change, or network-policy change stops or invalidates the run.

### NETWORK_OPERATION approval

Authorizes protocols, destinations, peer behavior, and resource use. It must record:

- Allowed resolver and client digests.
- Allowed protocols, tracker and web-seed domains, address ranges, regions, and network zones.
- Independent switches for private-tracker announce, public-tracker announce, DHT lookup, DHT announce or write, peer-exchange receive and send, local discovery, direct-peer connection, inbound listening, metadata request, metadata response, content-derived protocol-metadata egress, payload-piece upload, and seeding. A generic DHT, peer, or protocol permission must not imply any other switch.
- Private-tracker constraints and credential references.
- Bandwidth, connection, storage, time, and cost limits.
- Effective time, expiry, revocation, and incident-stop conditions.

`NETWORK_OPERATION` approval does not establish license or entitlement. Reference-only reacquisition requires current `VERIFIED` signed user-reproduction and source-distribution determinations. `DOWNLOAD_ONLY` fixes metadata response, content-derived protocol-metadata egress, payload-piece upload, and seeding switches to denied; a manifest or approval cannot relax them, and an implementation that cannot enforce the denial must block. In any other profile, content-derived protocol-metadata egress requires a current `VERIFIED` signed determination permitting that disclosure and the matching network switch. A peer-to-peer retrieval that may upload payload data additionally requires a current signed determination permitting redistribution, a `NETWORK_OPERATION` approval permitting payload-piece upload, and a current `SEED_UPLOAD` approval. Public tracker or DHT announcement, including an announce performed only to download, additionally requires a current signed determination permitting public announcement or content-identity disclosure, the exact public-announce network switch, and a current `PUBLIC_ANNOUNCEMENT` approval. If any required determination, switch, or approval is absent, the resolver must select an approved non-upload and non-public route or block.

### SEED_UPLOAD approval

Authorizes payload upload for explicitly named content IDs, representation IDs, swarms, peer scopes, network profiles, byte/rate budgets, lease duration, and shutdown conditions. It is separate from `NETWORK_OPERATION` approval and rights authority, is absent by default, and must not resume after restart, expiry, revocation, or policy drift without a new current approval.

### PUBLIC_ANNOUNCEMENT approval

Authorizes public tracker or DHT announcement, publication of a public swarm locator, or other disclosure of an infohash or participation state. It binds the exact torrent or swarm identities, public mechanisms, network profile, disclosure scope, duration, and revocation behavior. It is operational authority, not a rights grant: execution also requires a current signed rights determination explicitly permitting the public announcement or disclosure and a current `NETWORK_OPERATION` approval enabling the exact announce and content-derived egress switches. It is separate from SEED_UPLOAD: public announcement may be forbidden even when upload to an allowlisted private peer is permitted. Public seeding requires all three operational approvals plus current signed redistribution and public-disclosure determinations.

Every approval binds:

- Approval type and independently addressable approval ID.
- Exact resource revision covered by the signature.
- Canonical digest of the complete approval payload.
- Signature, signature algorithm, signing-key identity, and signing-key version.
- Authority revocation epoch, freshness proof, trusted-time evidence, not-before time, expiry, and maximum allowed freshness age.
- Approver identity and authentication method.
- Asset, component, or bounded selector.
- Root subject digest when the approval concerns a specific observed object.
- Authorized operation set and, for omission or restore acceptance, explicit component-scoped authorized outcomes. A generic allowed claim or job-local approval flag is forbidden.
- Validator-profile and threshold digests when validation is part of the approval.
- Bound policy, model, source, metadata-binding, network-profile, rights-evidence and rights-determination, placement-generation, GC-plan, preflight, threshold, and component-outcome revisions and canonical digests as applicable.
- Reason, issue time, and signed revocation-record references.
- Bulk-approval permission.
- Fallback policy.

Approval references always contain approval ID, exact resource revision, and canonical digest; resolving only a latest mutable record is forbidden. An approval is effective only while its signature, signing-key status and version, authority revocation epoch, freshness proof, trusted-time evidence, scope, bound revisions, not-before time, and expiry validate. Approvals become stale when any bound input changes. Before every external-network or destructive commit, the worker revalidates the applicable approval references and fails closed on clock rollback or unavailable freshness authority. LLM output cannot be the approving authority.

## 15. Fallback policy

Fallback must be ordered and machine-enforceable.

Illustrative actions:

~~~json
{
  "on_source_unavailable": [
    "TRY_NEXT_INDEPENDENCE_GROUP",
    "TRY_STORED_REPLICA",
    "BLOCK"
  ],
  "on_source_drift": [
    "QUARANTINE_CANDIDATE",
    "TRY_NEXT_SOURCE",
    "PROMOTE_WHILE_ORIGINAL_EXISTS",
    "BLOCK"
  ],
  "on_validation_review": [
    "QUARANTINE_CANDIDATE",
    "REQUEST_HUMAN_RESTORE_ACCEPTANCE",
    "BLOCK_ON_TIMEOUT"
  ],
  "on_validation_fail": [
    "TRY_NEXT_SOURCE",
    "TRY_EXACT_FALLBACK",
    "BLOCK"
  ],
  "on_validator_unavailable": [
    "RESTORE_RETAINED_VERIFIER",
    "REBUILD_PINNED_VERIFIER",
    "BLOCK"
  ],
  "allow_unlisted_component_outcomes": false,
  "never_overwrite_on_nonpass": true
}
~~~

For BitTorrent and other metadata-first retrievals, the ordered fallback must distinguish metadata acquisition from payload acquisition:

1. Load retained authenticated metainfo and piece layers.
2. Fetch metainfo from authenticated mirrors and verify every required full infohash.
3. Acquire metadata through approved tracker, DHT, or direct-peer routes and verify it before file selection.
4. Acquire pieces through approved peers or web seeds and verify torrent-native hashes.
5. For a validated hybrid torrent, try the alternate swarm without relaxing the final independent whole-file hash.
6. Try an independent provider or HTTP source bound to the same original whole-file identity.
7. Restore a retained replica or promote protection while original bytes still exist.
8. Quarantine mismatched or incomplete candidates and block.

No fallback may silently search for and accept a same-name torrent, change selected-file mapping, lower the requested claim, enable DHT or upload without approval, or overwrite a destination with a non-passing candidate.

## 16. Availability and staleness

Reference and recipe states include:

~~~text
candidate
-> verified
-> approved
-> degraded
-> source-drift or validator-stale
-> unavailable
-> promoted-for-stronger-protection
~~~

Degradation conditions include:

- Revalidation overdue.
- Authentication failure.
- Loss of independent sources.
- Changed source digest.
- License or region change.
- Restore time outside policy.
- Missing decoder, model, dictionary, or runtime.
- Changed validator implementation or threshold.
- Failed restore or rebuild drill.
- Stale approval.
- BitTorrent metainfo no longer retained or reacquirable.
- Missing or inconsistent v2 piece layers.
- No metadata-serving peer, no complete seed, or incomplete required-piece coverage.
- Private tracker entitlement, passkey, or approved network route unavailable.
- Selected-file map, torrent format, or full infohash disagreement.

## 17. Restore result states

Required successful and review states:

- RESTORED_ORIGINAL_BIT_EXACT
- RESTORED_TORRENT_V1_ORIGINAL_BIT_EXACT
- RESTORED_TORRENT_V2_ORIGINAL_BIT_EXACT
- RESTORED_TORRENT_HYBRID_ORIGINAL_BIT_EXACT
- RESTORED_SOURCE_EQUIVALENT
- RESTORED_NORMALIZED_CONTENT_EQUIVALENT
- RESTORED_PERCEPTUALLY_EQUIVALENT
- RESTORED_FUNCTIONALLY_EQUIVALENT
- RESTORED_SEMANTICALLY_EQUIVALENT
- VALIDATION_REVIEW_REQUIRED
- VALIDATION_INCONCLUSIVE

Required non-success interrupted or incomplete states:

- RESTORE_PARTIAL
- RESTORE_CANCELLED
- RESTORE_ROLLBACK_REQUIRED
- RESTORE_DESTINATION_DIVERGED

Required retrieval-stage states:

- RETRIEVAL_IDENTITY_VERIFIED
- RETRIEVAL_METADATA_VERIFIED
- RETRIEVAL_REQUIRED_PIECES_VERIFIED
- RETRIEVAL_SELECTED_FILES_EXACTLY_VERIFIED
- RETRIEVAL_QUARANTINED

Required freshness states:

- FRESHNESS_VERIFIED
- FRESHNESS_UNVERIFIED_OFFLINE
- FRESHNESS_ROLLBACK_DETECTED
- FRESHNESS_FORK_DETECTED
- BLOCKED_FRESHNESS_VERIFICATION

`FRESHNESS_UNVERIFIED_OFFLINE` is a serialized non-success freshness result, not an alias for `VALIDATION_INCONCLUSIVE`. It records that signatures, hashes, and internal ancestry were verified while global head freshness could not be established, together with the unavailable-witness reason, highest-seen-state reference when available, and exact non-destructive actions permitted by policy or a current `FRESHNESS_RECOVERY_EXCEPTION`.

Required failure and blocked states:

- BLOCKED_MISSING_SOURCE
- BLOCKED_AUTHENTICATION
- BLOCKED_LICENSE_OR_REGION
- BLOCKED_VALIDATOR_UNAVAILABLE
- BLOCKED_RIGHTS_DETERMINATION_MISSING
- BLOCKED_RIGHTS_DETERMINATION_NOT_VERIFIED
- BLOCKED_RIGHTS_EVIDENCE_STALE
- BLOCKED_NETWORK_OPERATION_APPROVAL
- BLOCKED_SEED_UPLOAD_APPROVAL
- BLOCKED_PUBLIC_ANNOUNCEMENT_APPROVAL
- BLOCKED_TORRENT_METADATA_UNAVAILABLE
- BLOCKED_TORRENT_NO_PEERS
- BLOCKED_TORRENT_INCOMPLETE_SWARM
- BLOCKED_PRIVATE_TRACKER_AUTHENTICATION
- FAILED_HASH_MISMATCH
- FAILED_SOURCE_IDENTITY_DRIFT
- FAILED_TORRENT_METAINFO_AUTHENTICATION
- FAILED_TORRENT_INFOHASH_MISMATCH
- FAILED_TORRENT_PIECE_HASH_MISMATCH
- FAILED_TORRENT_MERKLE_PROOF
- FAILED_TORRENT_FILE_MAP_CONFLICT
- FAILED_TORRENT_PATH_SAFETY
- FAILED_RECIPE
- FAILED_COMPONENT_REQUIREMENT
- UNRECOVERABLE

Every result includes the exact requested selection and scope, requested and achieved content claims, observed `capture_consistency_level` and its evidence reference, `requested_filesystem_restore_claim`, `achieved_filesystem_restore_claim`, freshness state and exact RecoveryHeadWitness reference or absence reason, per-component acceptable outcomes, ordered fallback actions taken, component outcomes, validator profile, acquisition evidence, mismatches, user-reproduction authority, source-distribution authority, the exact revision and canonical digest of every applicable `NETWORK_OPERATION`, `SEED_UPLOAD`, `PUBLIC_ANNOUNCEMENT`, or `FRESHNESS_RECOVERY_EXCEPTION` approval, and any component-scoped restore acceptance.

`RESTORE_PARTIAL` means fewer components than the recorded requested selection completed and lists every restored, skipped, blocked, failed, and still-pending requested component. A deliberately narrow selection that fully passes is a normal `RESTORED_*` result with that exact requested scope, not `RESTORE_PARTIAL`. `RESTORE_CANCELLED` records the cancellation actor or policy, durable checkpoint, staging cleanup or preservation decision, and rollback outcome and is never success. `RESTORE_ROLLBACK_REQUIRED` means destination mutations remain and an authenticated rollback plan requires execution or approval. `RESTORE_DESTINATION_DIVERGED` means the destination no longer matches the preflight baseline or mutation journal, so automatic continuation or rollback is unsafe.

Where any staging or destination write occurred, every interrupted, incomplete, rollback-required, or diverged result serializes authenticated staging-object references and a destination-mutation journal reference and digest covering created, replaced, renamed, merged, skipped, quarantined, externally changed, and rolled-back paths. None of these four states is snapshot success.

Snapshot success is true only when every required entry satisfies its own explicit contract. Claims are not globally ordered.

## 18. Asset, content, representation, namespace, and physical placement

The manifest keeps identity, namespace reconstruction, and physical storage placement separate:

- `asset_id` is stable across rename, move, remount, alternate restore root, and version history when policy considers the item the same user-facing asset.
- `content_id` is an immutable digest-and-length identity for one exact plaintext byte sequence. Any byte change creates a new content ID.
- `representation_id` identifies one immutable representation graph node and binds transformation class, output content ID, provenance, and dependencies. A backend-only move does not change it.

Source replacement may preserve the asset ID but creates the appropriate new content and representation IDs. Split, merge, extraction, and composition create new asset IDs unless an explicit domain policy defines a version relationship; the relationship edge is always recorded.

### 18.1 Namespace reconstruction records

Namespace reconstruction describes the logical tree to materialize and contains no backend locator. Its authenticated record set includes:

- Namespace record-set ID, schema version, generation, canonical digest, signature, and parent digest.
- Asset, content, representation, entry, root-subject, collection, and parent-entry IDs.
- Raw path components, normalized display path, logical root, entry type, link target, hard-link group, and placement role within the restored namespace.
- Case and Unicode normalization, collision policy, destination constraints, and required filesystem metadata.
- Valid-from and valid-through snapshot generations and the decision that created, renamed, moved, removed, or superseded the namespace entry.

Each logical entry carries a `namespace_reconstruction_ref` with record-set ID, generation, entry key, and digest. Offline restore reconstructs the intended namespace from this record set before resolving physical bytes.

### 18.2 Physical placement ledger

The append-only authenticated physical placement ledger maps immutable representation IDs to current and historical backend locations. It contains no user namespace authority. Each ledger generation is sealed by a signed checkpoint:

~~~json
{
  "checkpoint_id": "urn:uuid:...",
  "ledger_id": "urn:uuid:...",
  "schema_version": "restoreweave-placement-ledger/1",
  "payload": {
    "ledger_generation": 42,
    "parent_checkpoint_digest": "sha256:...",
    "entry_set_digest": "sha256:...",
    "candidate_manifest_digest": "sha256:...",
    "publication_domain_id": "...",
    "candidate_publication_generation": 42,
    "writer_id": "...",
    "coordinator_epoch": 7,
    "fencing_token": "...",
    "created_at": "..."
  },
  "authentication": {
    "canonical_payload_digest": "sha256:...",
    "signature_algorithm": "...",
    "signing_key_id": "...",
    "signing_key_version": 3,
    "signature": "..."
  }
}
~~~

`canonical_payload_digest` covers the canonical `payload`; the signature covers that digest and the checkpoint identity. The externally referenced checkpoint digest covers the complete signed checkpoint envelope. Genesis uses a declared null parent; every successor names exactly one parent checkpoint digest unless an explicitly approved fork record is being preserved.

Each placement creation is an immutable ledger event covered by `entry_set_digest`. It binds placement and event IDs, representation and content IDs, candidate manifest digest, publication domain and candidate generation, writer and fencing domain, creation generation, ownership mode, failure-independence group, initial state, immutability evidence, and exactly one ownership-mode payload. It must not contain a future `valid_through`, later readback result, or mutable current-health field.

An `ENGINE_MANAGED_OBJECTS` placement creation contains engine, adapter, repository, account, engine snapshot or restore-point handle, exact CaptureSetRecord ID and digest when source capture applies, authenticated engine receipt and digest, receipt fencing token, initial engine verification, independent RestoreWeave creation-time restore-verification reference, and reader Capsule Core ref. It does not require or promote engine-private object locators.

A `RESTOREWEAVE_PACKS` placement creation contains `RepositoryDriver` profile and repository, account, region, immutable backend locator, pack-object ID, encoded length, encoded-object digest, authenticated-encryption and index/footer digests, storage receipt, and initial independent readback result.

Verification, degradation, repair, draining, retention transitions, supersession, and retirement are new immutable events in later signed checkpoints. A closure event binds the placement ID, prior event digest, closing ledger generation, reason, approval and evidence references, final readback or loss evidence, and successor placement IDs. Effective `valid_through` and current state are derived from the signed event chain; no placement or earlier checkpoint is rewritten to close it.

A `physical_placement_ref` contains ledger ID, checkpoint ID, ledger generation, checkpoint digest, placement ID, and placement-creation-event digest. A current-resolution result additionally names the latest verified successor checkpoint and event digest. Historical signed manifests are not rewritten when placement state or location changes.

### 18.3 Recovery artifact placements

Recovery bootstrap artifacts have physical placements without pretending to be user assets or ordinary content representations. An immutable `RecoveryArtifactPlacement` event stream binds:

- Artifact kind and digest: `RECOVERY_BOOTSTRAP_SEED`, `RECOVERY_BOOTSTRAP_ENVELOPE`, `RECOVERY_HEAD_WITNESS`, `CAPSULE_CORE`, `CONTROL_PLANE_RECOVERY_SET`, `BOOTSTRAP_SEED_SUCCESSOR_RECORD`, witness policy or key-history record, epoch transition, fork-resolution record, or retained raw fork evidence.
- Publication domain and, when applicable, publication, envelope, witness, Seed, or Core generation.
- Storage adapter, backend locator, account or trust domain, region or site, physical medium or custody reference, and failure-independence group.
- Encryption and key-recovery references without plaintext secrets.
- Immutability, write, flush, seal, custody, replication, and independent full-readback evidence.
- Placement creation event, predecessor placement, successor placement IDs, reason, trusted time, approval where required, and signed checkpoint identity.

Required lifecycle states are `ACTIVE`, `GRACE`, `QUARANTINED`, `RETAINED_EVIDENCE`, and `RETIRED`. `ACTIVE`, `GRACE`, and `QUARANTINED` placements remain payload-liveness roots. `RETAINED_EVIDENCE` preserves the authenticated artifact bytes or a separately qualified compact validation anchor but does not by itself keep superseded payload placements live. `RETIRED` requires a complete cold-verified successor lineage and retains the signed placement and retirement evidence even when the artifact payload is collectible.

Creation, verification, degradation, repair, custody change, retention change, supersession, quarantine, evidence-only retention, and retirement are immutable events. Any event that changes authoritative discovery, eligibility, health, supersession, retention, quarantine, or retirement state creates a new signed physical-placement checkpoint and triggers the bootstrap-head advancement rules in Section 19.

## 19. Deferred legacy enterprise recovery closure

> **Superseded for RW-MVP-1:** This section preserves the earlier Capsule, Seed, Envelope, Witness, and control-plane closure design for possible later profiles. The current MVP recovery closure is the RRF companion plus portable signed publication commit defined in Section 20.

### 19.1 Capsule Core

A Capsule Core is an immutable, content-addressed package containing recovery code and dependencies that are independent of any one snapshot publication. The READY_TO_COMMIT manifest references required cores by:

- Core ID, schema and core version, content digest, signature, object reference, and media type.
- Supported platforms and architectures.
- Included manifest reader, repository reader, resolver, parser, validator, decoder, migration, and restore-runner identities.
- Lockfiles, fully closed build inputs, containers, models, dictionaries, schemas, configuration schemas, conformance vectors, and trust roots.
- Credential and wrapped-key recovery procedures without raw secrets.
- Minimum runtime, storage, privilege, rights, and network requirements.
- Clean-room execution and verification history.

A Capsule Core must not reference a snapshot manifest, publication record, placement generation, or Recovery Bootstrap Envelope. Updating it creates a new content digest; previously signed manifests keep their original core references.

For a declared offline recovery closure, every required core and all of its executable dependencies must be embedded in the closure or stored as authenticated protected objects reachable through the physical-placement root committed by the publication or a verified signed successor lineage. `EXTERNAL_ONLY`, registry-only, DNS-only, or network-rebuild-only cores are forbidden. Reproducible rebuild is sufficient only when the complete source, toolchain, dependencies, signature-verification tooling, non-authoritative trust metadata, and build instructions are inside the offline closure. Any trust anchor that authenticates that closure must still be retained independently; a key carried only by the closure cannot establish its own trust.

The closure must include at least one independently replicated Recovery Bootstrap Seed in a documented baseline format that can be extracted without a RestoreWeave-specific repository reader, codec, plugin registry, network service, or the Capsule Core it is intended to load. The seed contains exact signed envelope and current RecoveryHeadWitness copies, pinned subordinate trust anchors, canonicalization and signature-verification material, the minimum manifest and placement-checkpoint reader, object-extraction instructions, and any first-stage executable or source needed to load the initial Capsule Core. Its required decryption or key-unwrapping prerequisites must be available out of band and must not depend on that same core.

Every seed is content-addressed and carries a signed canonical inventory of member path, media type, length, and digest plus the envelope, witness, reader, and Capsule Core digests. The seed-signing root fingerprint and threshold policy must be independently known out of band or established by equivalent physically authenticated media; a root supplied only inside the seed is not trusted. Seed-contained subordinate anchors are accepted only after the seed signature and inventory validate against that external root. Clean-room starting assumptions therefore include the baseline extractor, the out-of-band root fingerprint or physical-authentication procedure, threshold policy, and required key-recovery material.

Illustrative authenticated seed header:

~~~json
{
  "seed_id": "urn:uuid:...",
  "seed_schema_version": "restoreweave-bootstrap-seed/1",
  "publication_domain_id": "...",
  "publication_record_digest": "sha256:...",
  "seed_generation": 7,
  "predecessor_seed_digest": "sha256:...",
  "envelope_digest": "sha256:...",
  "recovery_head_witness_digest": "sha256:...",
  "canonical_inventory_digest": "sha256:...",
  "members": [
    {
      "path": "recovery-envelope.json",
      "media_type": "application/json",
      "length": 0,
      "sha256": "..."
    },
    {
      "path": "recovery-head-witness.json",
      "media_type": "application/json",
      "length": 0,
      "sha256": "..."
    }
  ],
  "bootstrap_root_policy_digest": "sha256:...",
  "out_of_band_anchor": {
    "mode": "ROOT_FINGERPRINT|PHYSICALLY_AUTHENTICATED_MEDIA",
    "root_fingerprint": "sha256:...",
    "physical_seal_or_custody_ref": null
  },
  "signatures": []
}
~~~

The canonical seed signature payload covers seed ID, schema version, publication domain, publication-record digest, generation, predecessor-Seed digest, exact Envelope and RecoveryHeadWitness digests, complete ordered inventory, inventory digest, bootstrap-root policy digest, and the declared anchor mode. Genesis uses a declared null predecessor. Every non-genesis Seed names exactly one predecessor Seed in the same publication domain; a fork is preserved and resolved rather than silently collapsed. The `out_of_band_anchor` value inside the Seed is descriptive only: the operator or baseline verifier must compare it with the independently retained fingerprint, threshold-policy record, sealed-media identity, or chain-of-custody evidence before trusting any Seed-contained key. The complete signed Seed package has its own content digest for replica comparison and replacement history.

A signed `BootstrapSeedSuccessorRecord` is created after the new Seed has been independently replicated, passed cold package readback, and completed its `POST_PUBLICATION_BOOTSTRAP_DRILL`. It binds publication domain, predecessor and successor Seed digests and generations, exact contained Envelope and RecoveryHeadWitness digests, bootstrap-root policy digest, every required `RecoveryArtifactPlacement` and replica receipt, post-publication drill ID and evidence digest, cold-verification environment, failure-domain proof, trusted time, and signing quorum. Neither Seed points forward to this later record. Retirement, protection-health, and destructive-lifecycle gates accept a successor Seed only through a valid successor record or an equivalent signed record with the same bindings.

Capsule Core, repository-reader, codec, key-resolver, and bootstrap dependencies form an authenticated directed acyclic graph with an explicit topological load order. A core cannot count toward offline closure when its only placement requires that same core, a decoder available only inside it, or a later dependency to locate, decrypt, decode, or authenticate it. Clean-room drills must begin with only the declared Recovery Bootstrap Seed, baseline platform assumptions, and out-of-band recovery material.

### 19.2 ControlPlaneRecoverySet

A `ControlPlaneRecoverySet` is an immutable, signed, content-addressed recovery root for durable control-plane state that cannot be reconstructed from snapshot manifests alone. It contains or authenticates independently protected point-in-time copies of:

- Catalog and append-only event-log state required to reproduce durable facts and decisions.
- Published policies, ProtectionObjectives, recovery contracts, annotation schemas, user annotations and corrections, claim resolutions, processor-profile publications, AutomationGrants and revocations, approvals and revocations, rights evidence and determinations, legal and anomaly holds, configuration, schedules, service objectives, notification state, audit history, and requirement-applicability records.
- Source identities and transitions, CaptureSetRecords, key-recovery policy and ceremony evidence, worker-enrollment descriptors, credential-reference metadata without raw credentials, and fencing or reconciliation watermarks needed after control-plane loss.

The signed record binds publication domain, recovery-set generation, predecessor digest, capture time and consistency point, declared RPO and RTO, ordered component inventory and digests, encryption and key-recovery references, schema and reader Capsule Core digests, `RecoveryArtifactPlacement` IDs, ACL and residency policy, signing quorum, and independent full-readback evidence. Queue projections, agent conversational memory, and in-flight attempts are restored only as fenced evidence; they never revive stale leases or authority. Search indexes, embeddings, OCR, captions, summaries, and disposable enrichment are excluded and rebuilt from authoritative data. A `ControlPlaneRecoverySet` never points to a later Recovery Bootstrap Envelope or Seed.

### 19.3 Recovery Bootstrap Envelope

After atomic snapshot publication, RestoreWeave creates a separately signed Recovery Bootstrap Envelope. It points one-way to:

- The activated SnapshotPublicationRecord, its canonical digest, and the authenticated AtomicPublicationReceipt.
- The signed READY_TO_COMMIT manifest and manifest digest.
- The required Capsule Core IDs, digests, and protected-object discovery information.
- The exact `ControlPlaneRecoverySet` ID, generation, digest, reader requirements, and independently protected placements needed to recover durable control-plane state.
- The immutable namespace-reconstruction root committed by the publication.
- The latest signed physical-placement checkpoint plus an authenticated lineage proof back to the physical-placement root committed by the publication.
- The recovery-head witness policy, witness-set identities, minimum accepted witness epoch, and predecessor witness digest known before this envelope was signed. The current witness that binds this envelope is necessarily published afterward and must not be referenced backward.
- Required trust anchors, wrapped-key recovery references, repository discovery instructions, and offline closure declaration.

The manifest, Capsule Cores, ControlPlaneRecoverySet, SnapshotPublicationRecord, and AtomicPublicationReceipt must not point back to the envelope. The envelope is therefore not included in any digest it references and cannot create a signing cycle. A Recovery Bootstrap Seed may contain and point to an exact envelope copy, but the envelope must not point back to the Seed. The envelope is published to independently discoverable recovery roots or offline media, has its own generation and signature, and is replaced only by publishing a signed successor that preserves the immutable publication binding.

### 19.4 Bootstrap completion lifecycle

Every committed publication and every later authoritative checkpoint generation derives exactly one bootstrap-completion state:

~~~text
BOOTSTRAP_PENDING
-> ENVELOPED
-> WITNESSED
-> SEEDED
-> REPLICATED
-> COLD_VERIFIED
~~~

- `BOOTSTRAP_PENDING` begins when atomic publication commits, when a protected ControlPlaneRecoverySet generation changes, or when a later signed checkpoint changes authoritative discovery, eligibility, health, supersession, retention, quarantine, or retirement state.
- `ENVELOPED` requires a valid successor Recovery Bootstrap Envelope binding the exact publication, receipt, checkpoint, ControlPlaneRecoverySet, and required Capsule Cores.
- `WITNESSED` requires a threshold-valid RecoveryHeadWitness that binds that exact Envelope and checkpoint and advances valid witness lineage without rollback or fork.
- `SEEDED` requires a signed Recovery Bootstrap Seed containing that exact Envelope and Witness and binding its predecessor Seed and publication domain.
- `REPLICATED` requires policy-compliant independent `RecoveryArtifactPlacement` receipts for the complete required bootstrap set.
- `COLD_VERIFIED` requires a passing `POST_PUBLICATION_BOOTSTRAP_DRILL` and signed `BootstrapSeedSuccessorRecord` or equivalent evidence produced without the ordinary control plane or any location being retired.

`COMMITTED` and `COLD_VERIFIED` are independent facts. A committed generation below `COLD_VERIFIED` is not protection-healthy and cannot authorize source-only omission, retention reduction, placement or bootstrap-artifact retirement, garbage collection, deletion, destructive destination overwrite, or other destructive lifecycle effects. Preservation-only capture, replication, reconciliation, and bootstrap completion continue.

Before retiring any location or representation needed by an existing Recovery Bootstrap Envelope, Recovery Bootstrap Seed, ControlPlaneRecoverySet, Capsule Core, decoder, or validator, RestoreWeave must publish and independently replicate a successor physical-placement checkpoint, Recovery Bootstrap Envelope, RecoveryHeadWitness, Recovery Bootstrap Seed, `BootstrapSeedSuccessorRecord`, and required `RecoveryArtifactPlacement` records, verify their lineage and cold discovery, and reach `COLD_VERIFIED` before retiring the old path. A COMMITTED snapshot is not eligible for reference-only omission of its protected original or for garbage collection of its prior offline recovery path until the complete current bootstrap set has policy-required independently verified placements.

## 20. Portable MVP publication and deferred distributed publication

### 20.1 RW-MVP-1 portable publication commit

`RW-MVP-1` stores one exact payload restore point through a qualified mature exact repository and its `RepositoryDriver`, plus two small records with the `RECOVERY_CLOSURE` placement role in that repository. Restic may implement this profile, but the manifest contract does not depend on its private format:

1. The signed `PREPARED_CLOSURE` contains or authenticates the RRF root and binds the exact payload receipt, plan digest, platform-neutral capture-set digest, policy revision, explicit exclusions, and authenticated-metadata verification evidence.
2. After the prepared closure placement is durably observed and reconciled, RestoreWeave signs a `PublicationCommitRecord` with closure subtype `PUBLICATION_COMMIT`.
3. The commit record is stored and reconciled as the second recovery-closure placement. Its valid portable bytes are the logical commit point.

All three roles remain inside the same product-level repository placement and do not constitute redundancy or multiple-placement protection.

The `PublicationCommitRecord` binds at least:

- Schema and record kind.
- Publication ID and monotonic generation within its declared publication domain.
- RRF root digest.
- Payload restore-point or placement ID and authenticated payload receipt digest.
- Prepared-closure digest and authenticated prepared-closure placement receipt digest.
- Plan digest, capture-set ID and digest, and policy revision.
- Authenticated-metadata verification evidence digest.
- Writer identity, publication generation, coordinator epoch or equivalent fence, and signing time.
- Parent publication-commit digest when a predecessor exists.
- Signature and signing-key identity verifiable from an independent trust anchor.

The commit record must not contain its own storage-placement receipt. The recovery reader validates the signed record bytes and then observes the repository placement, avoiding a self-referential digest cycle. An exclusion is represented as an explicit non-recoverable decision and never contributes protected or verified coverage.

Clean-machine discovery starts from valid `PUBLICATION_COMMIT` records, resolves their bound prepared closures and payload restore points, and ignores orphan payloads or prepared closures. A crash or lost response after storage is reconciled by observing and validating existing records. Reconciliation may accept physically duplicated but equivalent repository objects while preserving exactly one logical publication. SQLite heads, search indexes, caches, and local publication pointers are rebuildable projections and do not establish commitment.

### 20.2 Deferred distributed-control publication state model

> **Superseded for RW-MVP-1:** The remainder of Section 20 preserves the former CAS head, AtomicPublicationReceipt, witness, and fork-resolution design for a possible later distributed-control profile. It is not part of the current release contract.

The deferred distributed-control profile uses this publication-state vocabulary:

~~~text
PREPARING
-> STAGED
-> VERIFIED
-> READY_TO_COMMIT
-> COMMITTED
~~~

Within that deferred profile, cancellation, verification failure, or abandonment are terminal job outcomes, not alternate names in the publication-state enum.

### 20.3 Deferred signed READY_TO_COMMIT manifest

The manifest is finalized, canonically serialized, digested, and signed only at `READY_TO_COMMIT`. It records readiness but cannot truthfully contain `COMMITTED`, `committed_at`, or a publication receipt that does not yet exist.

~~~json
{
  "snapshot_readiness": {
    "repository_id": "...",
    "generation": 42,
    "parent_generation": 41,
    "parent_publication_digest": "sha256:...",
    "writer_id": "...",
    "coordinator_epoch": 7,
    "fencing_token": "...",
    "state": "READY_TO_COMMIT",
    "preparing_at": "...",
    "staged_at": "...",
    "verified_at": "...",
    "ready_to_commit_at": "...",
    "object_set_digest": "sha256:...",
    "namespace_reconstruction_digest": "sha256:...",
    "physical_placement_ledger_digest": "sha256:...",
    "capsule_core_set_digest": "sha256:...",
    "approval_set_digest": "sha256:...",
    "verification_set_digest": "sha256:..."
  }
}
~~~

The readiness record and all listed roots are part of the one canonical unsigned manifest payload used for digesting and signing. Writer identity, coordinator epoch, generation, and fencing token cannot exist only in a mutable catalog.

### 20.4 Deferred SnapshotPublicationRecord, atomic activation, and receipt

Before compare-and-swap, the writer creates, canonically serializes, signs, stores, and reads back an immutable `SnapshotPublicationRecord`. At this point it is a dormant publication candidate: the repository publication state remains `READY_TO_COMMIT`, and possession of the record or its signature does not establish `COMMITTED` visibility.

~~~json
{
  "record_kind": "SNAPSHOT_PUBLICATION",
  "publication_id": "urn:uuid:...",
  "repository_id": "...",
  "generation": 42,
  "parent_generation": 41,
  "parent_publication_digest": "sha256:...",
  "manifest_ref": "...",
  "manifest_digest": "sha256:...",
  "writer_id": "...",
  "coordinator_epoch": 7,
  "fencing_token": "...",
  "intended_transition": "READY_TO_COMMIT_TO_COMMITTED",
  "publication_created_at": "...",
  "activation_intent": {
    "publication_domain_id": "...",
    "pointer_id": "published-generation",
    "expected_previous_generation": 41,
    "expected_previous_publication_digest": "sha256:...",
    "new_generation": 42
  },
  "object_set_digest": "sha256:...",
  "namespace_reconstruction_digest": "sha256:...",
  "physical_placement_ledger_digest": "sha256:...",
  "capsule_core_set_digest": "sha256:...",
  "approval_set_digest": "sha256:...",
  "verification_set_digest": "sha256:...",
  "signature": "..."
}
~~~

After the signed record is durable, RestoreWeave computes its canonical digest and validates that the digest resolves to the exact read-back bytes, the signature and signing-key state are valid, the activation intent matches the requested publication domain and pointer transition, the manifest digest resolves to the signed READY_TO_COMMIT manifest, and every committed root matches that manifest and its verified durable objects. Only then may RestoreWeave perform one fenced compare-and-swap from the expected old pointer tuple to `{generation, publication_record_digest}`.

The CAS authority must either repeat those validations atomically or consume a short-lived, single-use signed publication-validation token bound to the exact publication-record digest, manifest digest, root set, publication domain, old pointer, new generation, writer, coordinator epoch, fencing token, and expiry. Validation and token consumption are part of the same atomic decision as the pointer update; an unvalidated, expired, replayed, mismatched, or unresolved record digest cannot become the authoritative head. A successful CAS activates the immutable record as the `COMMITTED` publication; a failed CAS leaves an unpublished candidate that has no restore or retention authority.

The backend CAS evidence is captured in a separate authenticated `AtomicPublicationReceipt`; it is never embedded in the pre-CAS publication record.

~~~json
{
  "receipt_id": "urn:uuid:...",
  "publication_id": "urn:uuid:...",
  "publication_record_ref": "...",
  "publication_record_digest": "sha256:...",
  "publication_domain_id": "...",
  "pointer_id": "published-generation",
  "expected_old_pointer": {
    "generation": 41,
    "publication_record_digest": "sha256:..."
  },
  "observed_new_pointer": {
    "generation": 42,
    "publication_record_digest": "sha256:..."
  },
  "writer_id": "...",
  "coordinator_epoch": 7,
  "fencing_token": "...",
  "publication_validation_token_digest": "sha256:...",
  "committed_at": "...",
  "backend_cas_receipt_ref": "...",
  "backend_cas_receipt_digest": "sha256:...",
  "reconciled_after_lost_response": false,
  "signature": "..."
}
~~~

The receipt binds the exact old pointer, new pointer and publication-record digest, successful fencing decision, observed commit time, and backend evidence. The CAS authority must durably retain or deterministically expose authenticated evidence for successful transitions by publication domain and pointer tuple. The receipt must be independently authenticated and durably replicated. If the worker crashes or loses the response after CAS, reconciliation reads the authoritative pointer and backend CAS evidence, validates the already stored signed record, and emits a receipt with `reconciled_after_lost_response=true`; it never changes or re-signs the manifest or publication record.

Publication requirements:

- Generation is monotonic within an explicitly identified publication domain.
- Parent generation and parent publication digest match the committed predecessor except for a declared genesis or branch operation.
- The publication record exactly repeats and verifies the manifest digest and committed root digests; disagreement blocks publication visibility.
- The CAS transition cannot advance until the exact signed record, manifest, activation intent, and committed roots have passed validation bound to that same record digest.
- A stale fencing token cannot advance the atomic pointer; a receipt whose fencing evidence does not validate cannot corroborate the commit or authorize dependent post-commit actions.
- Reachability from the authoritative publication pointer, either directly or through the authenticated parent chain of a later publication, establishes `COMMITTED` for the exact digest of a valid signed SnapshotPublicationRecord. A signed but unreachable candidate is not authoritative. Missing post-CAS receipt material triggers reconciliation and may block dependent operations, but it does not roll back or make the pointer transition ambiguous.
- STAGED, VERIFIED, READY_TO_COMMIT, failed-CAS, and abandoned candidate data are invisible to restore, retention, deletion, and normal garbage collection. Unpublished candidates are protected only by active-job roots or an explicit staged-object grace period and become collection-eligible afterward.
- A lost response after successful compare-and-swap is reconciled from the pointer, already stored signed record, and backend CAS evidence, not by performing a blind second CAS or modifying signed objects.
- Pre-publication namespace-reconstruction records and physical-placement records bind the candidate generation, manifest digest, and fencing domain; they must not point to the later SnapshotPublicationRecord and create a backward digest cycle.
- Restore results, child generations, AtomicPublicationReceipts, and Recovery Bootstrap Envelopes identify the exact activated publication-record digest and fencing domain used. Physical-placement successors also carry an authenticated lineage to the placement root committed by that publication.

The manifest signature proves its READY_TO_COMMIT contents. The staged SnapshotPublicationRecord binds those contents to one intended atomic transition. The authoritative pointer establishes that the transition committed, and the AtomicPublicationReceipt preserves the backend and observed-time evidence. None of these authorities substitutes for another.

### 20.5 Deferred head freshness, rollback, and fork evidence

A valid signed parent chain proves ancestry but does not prove that a repository pointer or Recovery Bootstrap Envelope is the newest one ever published. After each publication, receipt reconciliation, ControlPlaneRecoverySet generation change, envelope replacement, witness-epoch transition, or authoritative physical-placement checkpoint event affecting discovery, eligibility, health, supersession, retention, quarantine, or retirement, RestoreWeave publishes a successor Recovery Bootstrap Envelope when any bound digest changes, then publishes a monotonic signed `RecoveryHeadWitness` to a policy-defined witness set outside the primary repository failure domain, then creates and independently replicates a successor Recovery Bootstrap Seed containing that exact Envelope and Witness. The Seed passes cold package readback and a `POST_PUBLICATION_BOOTSTRAP_DRILL` before the BootstrapSeedSuccessorRecord closes the lineage at `COLD_VERIFIED`.

~~~json
{
  "witness_id": "urn:uuid:...",
  "witness_schema_version": "restoreweave-recovery-head/1",
  "repository_id": "...",
  "publication_domain_id": "...",
  "witness_epoch": 3,
  "head_generation": 105,
  "previous_head_witness_digest": "sha256:...",
  "epoch_transition_record_digest": null,
  "publication_generation": 42,
  "publication_record_digest": "sha256:...",
  "atomic_publication_receipt_digest": "sha256:...",
  "envelope_generation": 7,
  "envelope_digest": "sha256:...",
  "physical_placement_checkpoint_digest": "sha256:...",
  "observed_at": "...",
  "trusted_time_evidence_ref": "...",
  "witness_set_id": "...",
  "signatures": [
    {
      "witness_member_id": "...",
      "signing_key_id": "...",
      "signing_key_version": 2,
      "signature": "..."
    }
  ]
}
~~~

The witness policy defines signer identities, signature threshold, failure-independence requirements, monotonic storage, and recovery rules. At least one required witness must be independent of the data repository, normal control-plane credentials, and primary envelope location. A witness is valid only when its head generation increases within the same witness epoch, or it is the first witness admitted through a valid epoch transition; its predecessor or transition binding must match, its publication record and receipt must validate, and its envelope and placement checkpoint must form the required one-way lineage. A Recovery Bootstrap Seed carries the exact envelope and current witness bytes together; the envelope does not point to that later witness.

Every reader persists the complete highest valid signed witness bytes or an authenticated immutable reference to them, plus at least `(witness_epoch, head_generation, witness_digest, previous_head_witness_digest, epoch_transition_record_digest, witness_set_id, publication_generation, publication_record_digest, atomic_publication_receipt_digest, envelope_generation, envelope_digest, physical_placement_checkpoint_digest)` in tamper-evident local state when such state is available. It compares repository, envelope, seed, and witness-set observations before automatic action:

- A lower witness epoch than the highest seen is rollback evidence.
- Within the same epoch, a lower head generation than the highest seen is rollback evidence.
- A witness that continues its predecessor's epoch uses a null `epoch_transition_record_digest`. The first witness in a higher epoch requires a non-null digest of a valid signed `WitnessEpochTransitionRecord` and sets `previous_head_witness_digest` to the transition's exact prior-highest witness digest; later witnesses in that epoch use a null transition field and inherit it through their predecessor chain. The transition binds the old and new witness sets and threshold policies, reason, trusted-time evidence, new epoch, and recovery-authority quorum. If fork evidence exists, the transition also requires a `PublicationForkResolutionRecord`.
- The same witness epoch and head generation with a different witness, predecessor, publication record, receipt, envelope, or checkpoint digest is fork or equivocation evidence.
- A higher generation with a broken parent or witness chain is an unverified fork, not a newer truth.
- A repository pointer older than a valid independent witness is never used for new writes, retention changes, omission, or deletion.

Rollback or fork evidence freezes publication, migration, retention, and destructive restore actions; preserves all observed branches and raw witness evidence; and requires an explicit signed `PublicationForkResolutionRecord` from the configured recovery authority. The record binds every observed signed witness and digest, pointer observation, publication record, receipt, envelope, placement checkpoint, persisted highest-seen state, affected repository and publication domain, chosen continuation, quarantined branches, evidence, reason, trusted time, authority quorum, new witness epoch, and exact CAS transition used to resume. It never rewrites or silently discards prior records.

When all independent witnesses and prior tamper-evident highest-seen state are unavailable, offline recovery can still verify signatures, hashes, and internal ancestry but cannot prove global freshness. The result is explicitly `FRESHNESS_UNVERIFIED_OFFLINE`; policy may allow non-destructive extraction to a new destination, but automatic overwrite, pointer advancement, omission, retention reduction, placement retirement, or deletion remains blocked until freshness is established. A current `FRESHNESS_RECOVERY_EXCEPTION` may authorize only its exact non-destructive scope and cannot substitute for a WitnessEpochTransitionRecord, PublicationForkResolutionRecord, or action-specific destructive approval.

### 20.6 Deferred freshness recovery authority records

A witness-epoch transition is a durable signed record, not a mutable policy update:

~~~json
{
  "record_kind": "WITNESS_EPOCH_TRANSITION",
  "transition_id": "urn:uuid:...",
  "repository_id": "...",
  "publication_domain_id": "...",
  "prior_highest_witness": {
    "witness_epoch": 3,
    "head_generation": 105,
    "witness_digest": "sha256:...",
    "witness_set_id": "...",
    "witness_policy_digest": "sha256:..."
  },
  "new_epoch": 4,
  "new_witness_set_id": "...",
  "new_witness_policy_digest": "sha256:...",
  "fork_resolution_record_digest": null,
  "reason": "...",
  "evidence_set_digest": "sha256:...",
  "trusted_time_evidence_ref": "...",
  "recovery_authority_policy_digest": "sha256:...",
  "signatures": []
}
~~~

The transition requires the threshold recovery authority named by `recovery_authority_policy_digest`, which must be independent of ordinary publication writers, operators, storage credentials, delete credentials, and the old and new witness services. It cannot choose among competing branches unless `fork_resolution_record_digest` identifies a valid `PublicationForkResolutionRecord`. It grants no restore, omission, retention, placement-retirement, pointer-update, or deletion authority by itself. The first RecoveryHeadWitness in `new_epoch` must reference the transition digest and name `prior_highest_witness.witness_digest` as its predecessor; all later witnesses inherit the transition through that signed predecessor chain.

A publication-fork resolution is separately signed and preserves every observed branch:

~~~json
{
  "record_kind": "PUBLICATION_FORK_RESOLUTION",
  "resolution_id": "urn:uuid:...",
  "repository_id": "...",
  "publication_domain_id": "...",
  "prior_witness_epoch": 3,
  "persisted_highest_seen_state_digest": "sha256:...",
  "observed_branches": [
    {
      "branch_id": "branch-a",
      "head_witness_digest": "sha256:...",
      "pointer_observation_digest": "sha256:...",
      "publication_record_digest": "sha256:...",
      "atomic_publication_receipt_digest": "sha256:...",
      "envelope_digest": "sha256:...",
      "physical_placement_checkpoint_digest": "sha256:...",
      "disposition": "CONTINUE|QUARANTINE"
    }
  ],
  "chosen_continuation_branch_id": "branch-a",
  "evidence_set_digest": "sha256:...",
  "reason": "...",
  "new_witness_epoch": 4,
  "authorized_resume_cas": {
    "expected_old_pointer_generation": 42,
    "expected_old_publication_record_digest": "sha256:...",
    "new_pointer_generation": 43,
    "new_publication_record_digest": "sha256:..."
  },
  "trusted_time_evidence_ref": "...",
  "recovery_authority_policy_digest": "sha256:...",
  "signatures": []
}
~~~

The resolution must enumerate every observed signed branch and raw evidence digest, mark exactly one branch `CONTINUE`, quarantine all others, and bind the exact resume transition. It requires the configured threshold recovery authority; normal writer, restore, omission, network, storage, or deletion authority is insufficient. The record cannot rewrite history, make quarantined branches collectible, or imply any destructive approval. Quarantined branches remain protected roots until a separate retention or deletion decision is valid after the new witness epoch is established.

## 21. Security and portability

- Manifests are authenticated and versioned.
- Historical manifests cannot be rewritten by normal backup credentials.
- Secrets are referenced, not embedded.
- Remote manifests, recipes, validators, and file content are untrusted inputs.
- Restore uses signed allowlisted executable identities and mandatory capability-sandboxed resolvers and plugins; arbitrary command strings are never authority.
- BitTorrent peers, trackers, DHT replies, metainfo, paths, symlinks, and web seeds are untrusted inputs and must be parsed with size, depth, path, and resource limits.
- Rights determination, `NETWORK_OPERATION`, `SEED_UPLOAD`, `PUBLIC_ANNOUNCEMENT`, and restore acceptance are separate signed authorities and must not be inferred from one another.
- Required validators and decoders are retained, reproducibly rebuildable from a complete retained closure, or both. Declared offline closure never depends on external-only Capsule Cores or registries.
- Model and metric supply-chain identities are part of the security boundary.
- The manifest format remains usable without the original database, AI model, or UI.
- Staging data cannot replace destination data until the requested identity and consistency contracts pass.
