# Security and Threat Model

> **Profile status:** This document preserves the broad cross-phase threat inventory. As frozen by the [MVP and Operator Contract](mvp-and-operator-contract.md), `RW-MVP-1` is a single-node, single-operator, single-workspace managed-archive profile for Linux/NAS deployments. It protects one local or mounted filesystem root through an honest generic capture profile, one mature exact deduplicating and compressing repository engine reported as one placement, hybrid lexical/structured search plus the bundled local ONNX/zvec semantic generation, a portable namespace and recovery closure, CLI, and a read-only local `stdio` MCP adapter. Platform- and engine-specific implementations qualify independently and never define product identity or a global release gate. REST, WebUI, A2A, remote AI, multimodal search, retrieval, public plugins, multitenancy, multiple placements, destructive lifecycle operations, and enterprise or cold-media custody are later-profile concerns.

## 1. Security objective and profile boundary

Across its possible profiles, RestoreWeave may process hostile files, media, archives, models, manifests, plugins, network sources, and AI output while holding access to important local data and recovery systems. `RW-MVP-1` deliberately has a narrower attack surface: it inventories one local or NAS-mounted tree, runs bounded default identification and metadata/text processors, preserves readable unknown content exactly, builds a rebuildable baseline search index, and writes exact protected bytes to one qualified repository engine.

The security objective is:

> A compromised input, plugin, source, worker, credential, or storage target must not silently reduce recovery coverage, escape its scope, corrupt trusted history, expose unrelated data, or make a weaker candidate appear exact.

### 1.1 RW-MVP-1 boundary

The first qualified profile has these security invariants:

- One authenticated local OS operator owns one local workspace.
- One plan covers one configured local or mounted root and binds either a retained immutable capture or an honestly declared validated-live capture basis. Snapshot consistency is claimed only when the selected `CaptureDriver` proves it; otherwise per-entry mutation evidence and any collection-level consistency limitation remain explicit.
- One configured mature exact repository is one placement. It is never described as redundant, independently resilient, or able to survive loss of that repository. A repository in the source failure domain is test-only; onboarding recommends a separate machine, remote target, or otherwise separate failure domain.
- Exact protection is the only source-recovery representation. A human- or published-policy-selected exclusion is explicitly unprotected and outside recoverable coverage; it is not a substitute, retrieval recipe, or smaller recoverable representation. Unknown, unsupported, ambiguously classified, and processor-failed readable bytes remain exact through `EXACT_FALLBACK`.
- The default suffix-then-magic identification, common metadata/text processing, content hashing, duplicate accounting, and baseline search surfaces are in scope. Their parsers and indexes are untrusted or rebuildable derivatives and cannot publish recovery truth.
- The CLI is the primary authority-bearing interface. The MVP MCP adapter is local `stdio`, opens no listener, and exposes only the qualified read-only surface. It cannot mutate plans, initialize or write a repository, restore a destination, read ambient host paths, access credentials, invoke arbitrary processors, or execute shell commands.
- No REST service, WebUI, hosted control plane, generative model, prompt loop, CLIP service, external retrieval path, P2P path, or public plugin marketplace is required or enabled by the MVP profile. The bundled local ONNX text-embedding worker and in-process zvec generation are the sole model-backed default: they run without network egress, remain rebuildable, and hold no recovery authority.
- Planning never writes the repository. Only application of an immutable plan may initialize or write the selected target, and the MVP exposes no automatic prune, delete, retention reduction, last-copy deletion, or autonomous exclusion operation.
- Exact planning, protection, verification, browse, and restore must pass with every optional Processor and every AI-related component disabled.

Portable recovery authority is intentionally small. A clean installation starts with an independently retained recovery reference, a separately available scoped credential source, an independent trust anchor, and the supported reader. It accepts a logical snapshot only from a valid signed `PUBLICATION_COMMIT` that binds the payload placement, prepared RRF closure, plan and capture or applied-inventory digests, authenticated-metadata evidence, and publication generation or fence. A prepared closure, payload snapshot, local catalog row, search index, or repository tag alone is not a commit.

### 1.2 Later profiles

Enterprise bootstrap chains, threshold custody, Recovery Bootstrap Seeds and Envelopes, RecoveryHeadWitness histories, ControlPlaneRecoverySets, Capsule Cores, multiple failure-independent placements, retrieval, additional embedding spaces, CLIP, multimodal ranking, P2P, distributed workers, remote APIs, and browser clients remain useful design directions. They are conditional requirements for later enterprise, cold-media, network, or semantic profiles and cannot gate or satisfy `RW-MVP-1`.

## 2. Protected assets

- Original and authoritative data.
- Signed manifests, plan and capture digests, placement receipts, prepared RRF closures, and signed `PUBLICATION_COMMIT` records.
- Recovery contracts, AutomationGrants, approvals, and audit history.
- Encryption, signing, and approval keys.
- Backend and source credentials.
- User paths, annotations, source and sidecar metadata, external assertions, model-generated claims, fingerprints, embeddings, and search indexes.
- Independently retained recovery references, independent trust anchors, scoped credential references, readers, decoders, models, and validators.
- In later enterprise or cold-media profiles only: Recovery Bootstrap Envelopes, Recovery Bootstrap Seeds, RecoveryHeadWitness records and transitions, ControlPlaneRecoverySets, Capsule Cores, and custody evidence.
- Storage replicas and immutable history.
- Rights, entitlement, and privacy records.

## 3. Trust boundaries

Required `RW-MVP-1` boundaries:

- Source filesystem to scanner.
- Local operator and CLI to the Core Command dispatcher.
- Read-only local MCP client to the bounded `stdio` adapter and Core Command dispatcher.
- Core to the selected generic or snapshot-capable `CaptureDriver` and its declared captured view.
- Untrusted file to parser or extractor.
- Default or optional processor package to its capability sandbox.
- Core to local operational state.
- Core to the pinned reference repository adapter, scoped credential source, and selected repository.
- Baseline metadata/text index to subject authorization and namespace resolution.
- Human approval to durable policy mutation.
- Repository bytes and recovery reference to the clean-machine reader and independent trust anchor.

Conditional later-profile boundaries include:

- Worker to control plane, database, queue, and storage backend.
- Retriever to external network.
- AI model to deterministic policy engine.
- Agent harness or future A2A adapter to a canonical control API.
- External enrichment provider to an egress broker and semantic catalog.
- Manifest to a broader restore kit or custody capsule.
- Torrent metadata, trackers, DHT, peers, and web seeds to quarantine.

Every boundary shipped by a profile must define input validation, authentication, authorization, resource limits, audit, and failure behavior.

## 4. Adversaries and failure actors

The design considers:

- Malicious file and archive authors.
- Compromised plugins, models, packages, registries, and update channels.
- Host malware and ransomware.
- Compromised source or storage credentials.
- Malicious or curious cloud and inference providers.
- Malicious trackers, peers, web seeds, and source mirrors.
- Authorized users making dangerous mistakes.
- Stale workers and replayed requests.
- Operators with excessive privileges.
- Network observers.
- Failing disks, networks, clocks, databases, and external services.

## 5. Filesystem traversal threats

Threats:

- Symlink, hardlink, mount, and reparse-point escapes.
- Time-of-check/time-of-use races.
- Device, socket, FIFO, and special-file access.
- Infinite recursion and mount-boundary traversal.
- Path reuse and inode reuse.
- Permission changes during scan.
- Snapshot-provider substitution, stale snapshot handles, or live-path fallback presented as a consistent capture.
- Source identity spoofing, cloned-volume confusion, watcher-journal rollback, reset, or overflow.

Requirements:

- Retain a trusted root handle for the complete capture attempt and perform enumeration, metadata inspection, `readlink`, open, and read through component-relative handle operations. Closing an ancestor and later reopening its absolute path is not continuous root authority.
- On Linux, use [`openat2`](https://man7.org/linux/man-pages/man2/openat2.2.html) with the qualified `RESOLVE_*` policy or an independently qualified equivalent. Final-component-only `O_NOFOLLOW` does not protect ancestor components, bind-mount replacement, or root substitution and is insufficient for authoritative capture.
- Do not use the legacy string-returning `SecureJoin` API for authoritative access. Its [upstream documentation](https://github.com/cyphar/filepath-securejoin#old-api) identifies a fundamental TOCTOU race. If a handle-returning resolver is adopted, pin its revision, qualify its kernel fallback and handle operations, and satisfy the license obligations of the exact source files used.
- Capture stable source, filesystem or volume, mount, configured-root, root-object, and snapshot or validated-live identity in an opaque `CaptureRootBinding`; runtime descriptor numbers and absolute exposure paths are not durable identity.
- Bound recursion. `RW-MVP-1` accepts one configured root. Crossing into nested mounts is disabled unless the source configuration and capture profile explicitly include them.
- Pin the entry type through a safe handle before any potentially blocking content open. Treat FIFOs, sockets, block and character devices, and other special files as metadata-only unless a separately qualified typed profile grants access; never discover the type only after an ordinary `O_RDONLY` open.
- Re-stat before and after read when no snapshot exists.
- Mark changing or inconsistent files for retry or exact preservation.
- Never interpret an incomplete scan as deletion.
- Bind every authoritative observation to an authenticated capture record, validated `CaptureRootBinding`, or applied-inventory basis. A missing, failed, stale, unchecked, or substituted root, mount, snapshot, or live basis cannot authorize namespace publication. A live capture cannot claim collection-wide atomicity it did not achieve.
- The generic capture profile must detect per-entry mutation where its contract says it can, preserve unresolved drift, and report its achieved consistency honestly. Failure of any optional snapshot driver blocks only that driver profile.
- Treat source adoption, replacement, relocation, decommission, and loss as signed lifecycle transitions. Journal invalidation requires a complete baseline before deletion evidence becomes authoritative.

## 6. Parser, archive, and media threats

`RW-MVP-1` requires bounded suffix-then-magic identification, qualified common metadata and text extraction, and a generic exact route. Deep recursive archive analysis, OCR, ASR, learned classification, additional embedding spaces, CLIP, and codecs remain optional or later. Every shipped parser, detector, and extractor remains inside this boundary. Unsupported or failed processing of readable bytes selects exact protection rather than omission or a blocking intelligence dependency.

Threats:

- Parser memory corruption.
- Decompression and archive bombs.
- Recursive containers.
- Malformed metadata and integer overflow.
- Polyglot, overlapping, trailing, concatenated, and parser-confusion inputs.
- Excessive thumbnails, frames, streams, symbols, or output.
- Malicious fonts, codecs, and embedded scripts.

Requirements:

- Run untrusted, memory-unsafe, third-party, or deep parsers in a capability-enforcing WASM runtime, hardened container, or OS sandbox; process separation alone is insufficient. A minimal bundled memory-safe type detector may remain in process only when it is non-recursive, allocation-bounded, fuzzed, and unable to change exact-fallback policy.
- Default to read-only input and no network.
- Prohibit shell command strings and interpolation from manifests or untrusted recipes. Native tools require signed allowlisted executable digests, structured argv, bounded environment variables, explicit filesystem roots, syscall or host-function restrictions, and child-process policy.
- Enforce input, output, recursion, file-count, CPU, RAM, disk, and time quotas.
- Enforce cumulative nested-container depth, expansion bytes, expansion ratio, member, page, frame, stream, symbol, and derivative limits at the host level rather than resetting limits per plugin invocation.
- Use safe temporary roots and no automatic execution.
- Preserve every valid or conflicting parser view, unclaimed byte range, and explicit ambiguous, polyglot, partial, encrypted, malformed, unsupported, unknown, and failed coverage state.
- Unknown and failed parsing defaults to exact preservation.
- Fuzz all security-sensitive parsers and canonicalizers.

## 7. Plugin and supply-chain threats

`RW-MVP-1` has no public plugin marketplace. Its immediate supply-chain boundary is the supported controller package, pinned reference repository engine and adapter, generic capture implementation, RRF reader, baseline index implementation, qualified default processors, and any explicitly enabled optional processor. Marketplace enrollment, third-party publisher governance, and retained historical decoder fleets are later-profile requirements.

Threats:

- Malicious or compromised plugin.
- Dependency confusion.
- Model or weight replacement.
- Unpinned container tags.
- License revocation or incompatible redistribution.
- Vulnerable historical decoder.

Requirements:

- Publish and verify the supported package manifest, component digests, compatibility matrix, SBOM or component inventory, licenses, and notices.
- Pin the reference repository engine, adapter, RRF reader, capture implementation, baseline index, and qualified processor versions and digests. Optional privileged snapshot helpers require separate signature, privilege, and compatibility qualification.
- Signed and content-addressed plugin packages.
- Explicit trust-root and publisher enrollment, signer rotation, revocation, compromise response, rollback and freeze protection, and trusted update metadata.
- Pinned code, model, runtime, container, dictionary, and dependency digests.
- SBOM, license, and notice records.
- Capability-based permissions.
- Install, enabled, quarantined, deprecated, revoked, and retained-for-restore states.
- Separate permission to execute new work from permission to decode historical data.
- Reproducible builds or retained binary artifacts.
- Vulnerability and license monitoring.
- Published scan cadence, severity policy, remediation SLO, and emergency quarantine behavior.
- Conformance and adversarial test vectors.

## 8. AI and prompt-injection threats

`RW-MVP-1` embeds no prompt loop or agent runtime. It ships a local ONNX embedding worker and in-process zvec generation as rebuildable discovery derivatives whose results resolve through host-owned subjects and namespace authorization. Exact protection must remain independent of every index. The qualified MCP surface is read-only and carries no approval authority. The threats and controls below apply to the default local semantic processing as well as later remote or multimodal providers.

Threats:

- Instructions embedded in content, metadata, images, OCR, subtitles, or retrieved context.
- Data exfiltration through model requests.
- Model hallucination presented as fact.
- Agent-generated policy broadening.
- Same model generating and approving a candidate.
- Agent-framework memory or task state presented as durable RestoreWeave truth.
- MCP tool annotations, A2A task state, or client confirmations presented as approvals.
- AI or external metadata overwriting user-authored semantics.
- Stale vector indexes leaking newly restricted content.

Requirements:

- Treat all content as untrusted data.
- Schema-validate structured model output.
- Separate facts, claims, decisions, and approvals.
- Do not grant the model delete, exclude, publish, secret-read, or arbitrary-network permissions.
- Remote Processor use is per-scope opt-in under an immutable `RemoteProcessorEgressProfile` and produces an egress receipt.
- Core records host-enforceable disclosure scope, destination, credential reference, budgets, input and output digests, truncation, output schema, and opaque Processor provenance digests. Provider accounts, models, prompts, token accounting, training terms, and routing remain inside the Processor or external harness rather than becoming Core policy fields.
- Apply the same host-enforceable egress, residency, minimization, validity, revocation, and purge boundaries to any authorized remote Processor or external semantic service.
- Use deterministic non-LLM policy enforcement.
- Require independent approval for semantic invariants and weaker recovery.
- Store every AI or external assertion in a provenance class separate from user annotations and observed source facts.
- Treat every agent output as a proposal bound to exact input revisions; deterministic validation and simulation are required before a policy draft or job can be created.
- Do not accept protocol-level confirmation, tool annotations, agent task completion, or framework voting as action-specific RestoreWeave approval.
- Apply query-time authorization after lexical or vector candidate retrieval and resolve every hit through a host-owned durable subject.

## 9. Command, repository-initialization, and publication threats

`RW-MVP-1` has no network control API, browser session, hosted identity, tenant boundary, or distributed worker fleet. Its relevant control boundary is the authenticated local operator, immutable command and plan records, the local catalog and baseline search projections, the read-only MCP adapter, and side effects performed through the selected capture and repository adapters.

MVP threats:

- A local process or MCP client acquiring repository, credential, arbitrary-path, shell, or mutation authority that was not granted.
- Replay, idempotency misuse, stale plan application, stale capture use, or a superseded attempt committing after its fence expired.
- Audit or SQLite projection tampering being mistaken for portable recovery truth.
- Planning initializing or writing a target.
- A crash during repository initialization leaving a partial repository, ownership marker, or ambiguous external outcome.
- A target acquiring an existing repository, conflicting identity, or unrelated data between planning and application.
- Blindly retrying ambiguous initialization or placement creation and creating conflicting effects.
- A payload snapshot or orphan `PREPARED_CLOSURE` being presented as a published logical RestoreWeave snapshot.
- A forged, replayed, stale-generation, wrong-repository, or mismatched `PUBLICATION_COMMIT` being accepted.
- A local publication pointer being treated as authority even when the portable commit chain does not validate.

RW-MVP-1 requirements:

- Bind the workspace to the authenticated local OS principal with least-privilege filesystem permissions. Keep credential values in a scoped host credential source; plans, events, MCP results, recovery references, and RRF records contain references or fingerprints, never plaintext reusable secrets.
- Bind every authority-bearing command and idempotency key to the operator, workspace, exact operation, immutable input digest, plan digest, capture digest, repository identity, and attempt fence as applicable.
- The local `stdio` MCP adapter is read-only in the qualified profile, opens no listener, and cannot answer an operator prompt or mint human decision authority.
- Planning may validate only the explicitly supplied target and must not write it. Application must revalidate target identity and conflict state immediately before initialization or placement creation.
- Repository initialization uses the semantic operations `InitializeTarget` and `ReconcileTargetInitialization`, an initialization-scoped idempotency key, and, where the backend permits, an atomic target ownership or initialization-intent marker bound to the plan, repository configuration digest, and attempt fence.
- After interrupted initialization, a matching marker plus a compatible initialized empty repository is accepted as the intended result. A matching marker plus no repository after a qualified observation barrier may be retried with the same logical identity. A repository without the matching marker, a conflicting repository identity, or unrelated target data blocks. An observation that cannot distinguish these states remains `RECONCILING` or terminates as `UNKNOWN_EXTERNAL_OUTCOME`; it never authorizes blind retry or publication.
- Placement creation and reconciliation use distinct role-scoped identities for `PAYLOAD` and `RECOVERY_CLOSURE` effects. Equivalent physical engine duplicates may reconcile to one logical receipt, but conflicting candidates block publication.
- After payload reconciliation and authenticated-metadata verification, store and reconcile the signed RRF artifact as a `RECOVERY_CLOSURE` placement with subtype `PREPARED_CLOSURE`. This artifact is necessary but not sufficient for publication.
- Create a portable signed `PublicationCommitRecord` with subtype `PUBLICATION_COMMIT` only after the payload receipt and prepared-closure receipt are reconciled. It binds the repository identity, RRF root, payload receipt, prepared-closure receipt, plan digest, capture digest, authenticated-metadata evidence, and publication generation or fence. Store and reconcile it as a second small `RECOVERY_CLOSURE` placement.
- A `PUBLICATION_COMMIT` cannot contain or authenticate its own placement receipt. Its signed content identifies the prior prepared effects; repository enumeration and content authentication prove the commit-marker placement separately.
- Clean-machine discovery starts only from valid `PUBLICATION_COMMIT` markers authenticated by the independently retained trust anchor. It then verifies every bound field before opening the prepared closure or payload. Orphan payloads, orphan prepared closures, unsigned tags, marker/closure/payload mismatches, wrong-repository receipts, invalid signatures, and stale or replayed generations remain invisible or blocked.
- Local publication pointers, SQLite rows, indexes, and status caches are rebuildable projections of valid portable commits. Deleting, rolling back, or forging a local projection cannot create or erase portable publication authority.
- Recheck target identity, plan and capture binding, credential scope, policy and decision authority, time freshness, idempotency identity, and fencing before every repository, restore-destination, or canonical-publication side effect. Unavailable or stale authority fails closed.

Later distributed, browser, enterprise, or agent profiles additionally require:

- Workspace ownership, RBAC or ABAC, least privilege, MFA, and break-glass procedures.
- Separate viewer, operator, policy-author, omission-approver, restore-approver, key-custodian, auditor, and administrator roles.
- Short-lived scoped service tokens and revocation.
- Signed approval and credential revocation epochs, bounded freshness, trusted-time evidence, monotonic attempt deadlines, and wall-clock rollback detection.
- CSRF, CORS, request-smuggling, and browser-session policy.
- Tamper-evident append-only audit export.
- Compare-and-swap publication projections and rollback or fork detection through the qualified RecoveryHeadWitness, Recovery Bootstrap Seed, Recovery Bootstrap Envelope, and snapshot-publication chain.
- Revision-checked signed resources for source identity transitions, anomaly-hold resolution, key-recovery policy changes, and service-SLO suppression. Ordinary job comments or UI state are not authority.
- AutomationGrants bound to exact operation, subject, plugin, destination, budget, risk, dynamic-selector drift, validity, and revocation scope. They cannot replace named destructive, weakening, upload, writeback, restore-acceptance, identity, key, or last-copy approvals.
- Northbound agent adapters restricted to the canonical API and a protocol-scoped principal, with no direct database, queue, worker-lease, repository, key, storage-locator, or arbitrary-path access.

## 10. Storage and ransomware threats

Threats:

- Compromised client deletes all versions.
- Source-side ransomware encrypts, truncates, renames, or rewrites large portions of the protected namespace and then appears as ordinary new data.
- An attacker suppresses or forges change-anomaly, dead-man, or notification evidence.
- Storage provider corrupts or withholds data.
- Shared key loss.
- Replicas share one failure domain.
- GC deletes live dependencies.

Requirements:

For `RW-MVP-1`:

- Treat the configured exact repository as exactly one placement and one repository failure domain. A remote or off-machine target is the onboarding default, but even an offsite target is not redundancy.
- Permit a same-source-volume repository only in an explicit test profile and report `AT_RISK_SAME_FAILURE_DOMAIN` or its stable equivalent.
- Use scoped repository credentials and never expose them through CLI machine output, MCP, logs, RRF, the prepared closure, or the recovery reference. The MVP supplies no prune, delete, or retention-reduction command and therefore grants no destructive repository authority through its public surface.
- Require signed role-specific placement receipts, authenticated readback, deterministic sampled verification, explicit full-byte verification, and a clean-install restore drill. Repository-engine success alone is not verification.
- Bind portable publication through the reconciled payload, `PREPARED_CLOSURE`, and signed `PUBLICATION_COMMIT` chain defined in Section 9.
- Detect repository corruption and fail the affected verification or restore claim. With one repository there may be no repair source; that absence must remain visible rather than becoming a false resiliency claim.
- Treat loss, withholding, credential loss, or irreparable corruption of the sole repository as loss of recoverability. Local catalog or source copies do not preserve the repository claim.
- Surface suspicious mass deletion, rename, truncation, or rewrite as plan evidence and require review for any newly weaker outcome. The exact default may protect the newly observed bytes, but the one-placement MVP does not claim independent ransomware survival.

Later resilience, enterprise, and cold-media profiles additionally require as applicable:

- At least one immutable or separately controlled generation.
- Separate write and destructive-delete credentials.
- Formal failure-domain modeling across placements.
- Corruption detection with at least one qualified repair source.
- Two-phase fenced garbage collection.
- Legal holds and active jobs as GC roots; `ACTIVE`, `GRACE`, and `QUARANTINED` RecoveryArtifactPlacements as payload-liveness roots; and ControlPlaneRecoverySets, Recovery Bootstrap Envelopes and Seeds, BootstrapSeedSuccessorRecords, RecoveryHeadWitness policies and history, fork evidence, Capsule Cores, and required dependency lineage as authentication-evidence roots until a signed disposition permits collection.
- Recovery keys and readers independent of each storage provider.
- Deterministic, version-bound change-anomaly detection over complete scan generations. The detector may create a signed system `ANOMALY_PRESERVATION_HOLD` but cannot delete, omit, or classify the newest bytes as safe.
- An active anomaly hold that preserves required pre-event history and blocks omission, retention reduction, placement retirement, and GC while exact capture of new observations continues.
- Separately signed, evidence-bound hold release. Missing anomaly telemetry is unknown rather than benign.
- Authenticated health watermarks sent to an independent dead-man monitor outside the source host and ordinary control plane; replay, rollback, non-advancement, and lateness are detectable without disclosing content or secrets.

## 11. Cryptographic threats

Threats:

- Key loss or compromise.
- Nonce reuse.
- Algorithm obsolescence.
- Publication-signing rollback or substitution.
- A recovery reference, credential, or later bootstrap artifact becoming unavailable or being encrypted only under an unavailable key.
- Deterministic or convergent encryption equality leakage.

Requirements:

For `RW-MVP-1`:

- The selected mature repository engine owns payload encryption, compression, chunking, deduplication, and private pack or object layout. RestoreWeave must not add a second convergent-encryption, neural-codec, or encrypted-pack layer around the MVP payload.
- Separate the local publication-signing identity from the repository credential source. Keep the signing private key and plaintext repository credential out of plans, events, repository metadata, RRF, prepared closures, commit records, recovery references, and MCP results.
- Export an independently retainable public trust anchor or authenticated root in the recovery reference. A verification key learned only from the `PUBLICATION_COMMIT`, prepared closure, payload repository, or companion metadata being authenticated is not independent trust.
- Verify the signature, signer identity, publication generation or fence, repository binding, and complete payload/closure binding of every `PUBLICATION_COMMIT` before clean-machine discovery treats it as authoritative.
- Report recovery readiness as degraded until the independently retained recovery reference, scoped credential source, independent trust anchor, and current clean-install drill have been proven together. A credential available only on the source host does not satisfy independent recovery readiness.
- Separate keys by purpose. Any RestoreWeave-owned authenticated encryption must provide crash- and retry-safe nonce uniqueness. A retry either reuses already committed identical ciphertext or advances to a new key and nonce generation; it never encrypts different plaintext or associated data under the same key and nonce.
- Support documented signing-key and credential rotation or replacement without rewriting historical truth, and retain the compatible reader and verification instructions needed for qualified restores.

Later enterprise and cold-media custody profiles additionally require as applicable:

- Multi-recipient or threshold recovery.
- Signed `KeyRecoveryPolicy` records for every offline-required key and credential, including repository recovery, signing roots, bootstrap roots, and backend account-recovery dependencies.
- Threshold-share and custodian independence, lifecycle, refresh, loss, compromise, replacement, and revocation rules. No ordinary host or single principal holds enough material to satisfy a threshold policy.
- Recovery ceremonies that produce authenticated success or failure evidence, key fingerprints or credential-validation results, and cleanup or zeroization evidence without logging shares, passwords, private keys, or reconstructed secrets.
- Quorum-loss and stale-ceremony health semantics even while encrypted replicas remain available.
- Independently authenticated Recovery Bootstrap Envelopes, Seeds, Witnesses, and custody transitions.
- Rotation, rewrap, compromise response, and historical restore testing. KEK-only compromise may use verified rewrap only when content keys were not exposed; DEK compromise requires verified payload re-encryption with new DEKs, nonce domains, placements, signed migration evidence, and retirement of compromised generations.
- Algorithm-agility migration, repository-scoped keyed identities where equality hiding matters, no global convergent encryption by default, and signed generation rollback detection.

## 12. Restore-time threats

Threats:

- Restoring malware or a compromised snapshot.
- Overwriting newer destination data.
- Path and metadata incompatibility.
- Executing recovered applications before validation.
- Accepting a weaker candidate as exact.

Requirements:

- `RW-MVP-1` restores only into a new empty isolated destination. A non-empty destination, overwrite, merge, rename-on-conflict, or in-place restore request blocks and requires a later qualified profile.
- Authenticate the selected `PUBLICATION_COMMIT`, prepared closure, payload binding, requested namespace, and exact content digests before reporting a restore as accepted or verified.
- Preflight destination capacity, privileges, emptiness, path collisions, and filesystem capabilities.
- Crash or cancellation leaves an explicitly isolated partial destination and never reports success. A retry must reconcile or restart through the immutable restore plan.
- Application or game execution is prohibited during acquisition and restoration. Restored executable content remains inert; malware or signature checks may add evidence but cannot silently substitute content.
- Requested and achieved claims displayed per component.
- No non-passing candidate overwrites a canonical destination.
- Partial and cancelled restores remain explicit.

Later profiles that permit mutation of an existing destination must define explicit overwrite, merge, rename, skip, atomic-replace, staging, rollback, retained-preimage, and write-ahead-journal behavior. Retrieval profiles must also quarantine downloaded sources and substitutes. Dynamic application validation requires separate signed authority and a disposable capability sandbox bound to exact executable identities and explicit child-process, device, filesystem, credential, resource, rollback, and network policy.

## 13. BitTorrent and magnet threats

This entire section is deferred from `RW-MVP-1` and applies only to a separately qualified retrieval or P2P profile.

### 13.1 Metadata and parser abuse

Enforce limits on URI size, parameter count, bencode depth, file count, path bytes, trackers, peers, retries, and time. Apply `max_bep9_info_bytes` to the handshake `metadata_size` and data-message `total_size` before allocation, with separate limits for complete torrent-file bytes and v2 piece-layer bytes because BEP 9 transfers only the `info` dictionary.

Verify the complete information dictionary against the pinned infohash before parsing file paths or allocating payload storage.

BEP 9 assembly requires one consistent positive size, exact 16 KiB non-final blocks, the exact final remainder, bounded indices, and rejection of inconsistent duplicates, overlaps, unsolicited blocks, size changes, and trailing bytes. A v2 or hybrid source requiring piece layers cannot satisfy portable metadata closure until those layers are retained and validated against the complete per-file pieces roots.

### 13.2 Path traversal

Reject unsafe components, normalization collisions, reserved names, symlink escapes, padding confusion, and case collisions before download.

### 13.3 SSRF

Trackers and web seeds use an egress broker with scheme, address, redirect, DNS-rebinding, port, and private-network policy. Workers have no ambient cloud credentials.

### 13.4 Privacy

Public torrent activity may expose IP addresses, infohashes, peer identity, file-size layout, and traffic patterns.

Every network mechanism is independently controlled:

- Tracker scrape.
- Private-tracker announce.
- Public-tracker announce.
- DHT `get_peers` lookup.
- DHT announcement.
- Peer connections.
- PEX.
- Local discovery.
- Inbound listening.
- Port mapping.
- Payload-piece upload.
- Torrent-metadata responses and other content-derived egress.

RestoreWeave must not claim anonymity.

Download-only guarantees zero payload-piece upload and zero content-derived metadata upload, not zero outbound control bytes. It may issue explicitly approved requests but must not serve torrent metadata, piece layers, derived swarm metadata, or payload pieces. Enforcement by the mandatory P2P Network Broker, optionally reinforced by an independent external egress boundary, plus packet-capture or equivalent flow verification is required; unsupported clients are blocked, and any profile that permits metadata serving is not download-only. Private torrents announce to one selected tracker at a time, connect only to peers returned by it, disable DHT, PEX, local discovery, `x.pe`, manual/direct hints, and non-tracker resume peers, and disconnect existing peers before tracker failover.

A public tracker or DHT announce requires separate `PUBLIC_ANNOUNCEMENT` approval even when performed to obtain peers. Metadata responses, raw information dictionaries, piece layers, and derived swarm metadata require a current `VERIFIED` signed `SOURCE_DISTRIBUTION_AUTHORITY` determination; transport approval alone is insufficient.

Each P2P job uses fresh broker-owned session state and an isolated network namespace per workspace and immutable network-profile version, or an equivalently proven state partition. DHT tables, peer and resume caches, PEX and local-discovery state, tracker state, NAT mappings, and credentials cannot cross workspace, swarm, or profile boundaries.

Public swarms cannot satisfy deletion recall, strict residency, regulated-content, or offline-closure guarantees. Policy blocks public P2P for content whose objective requires those properties.

### 13.5 Rights and redistribution

Operational approval does not create legal rights. Immutable rights evidence records observed facts and terms; separate signed rights determinations distinguish acquisition, reproduction, transformation, and redistribution authority.

No default public piracy-index integration is permitted. Public seeding requires explicit rights evidence and separate approval.

Content-derived metadata distribution and payload distribution are separately scoped. No operational approval or break-glass issuance mode can create or replace rights authority.

### 13.6 Poisoned and wrong content

Torrent piece validation proves correspondence to torrent metadata, not correctness relative to the user's original.

Final exact recovery requires the independent RestoreWeave whole-file hash. A valid but wrong-edition or malicious torrent remains quarantined.

### 13.7 Availability

Metadata resolution, peer count, or observed pieces do not prove complete recoverability. Only a clean full acquisition supports reference-only treatment.

Trusted peers have explicit enrollment, authenticated device identity, operator and failure-domain attribution, storage-only versus key-holding role, attestation state, possession-challenge policy, revocation, compromise and rekey behavior, location evidence, and offboarding. A self-signed possession claim is not an independent replica proof.

## 14. Multitenancy and deduplication

This section is not part of `RW-MVP-1`. The MVP has one local workspace and one local operator, no hosted identity, no team sharing, and no cross-workspace deduplication. Its local lexical/structured/zvec generations are single-workspace and rebuildable; remote, multimodal, and cross-workspace semantic services remain later profiles.

Threats:

- Cross-tenant existence or size inference.
- Shared-key compromise.
- Search-index ACL leakage.
- Dedup confirmation attacks.

Requirements:

- Tenant-scoped keys and authorization.
- No cross-tenant deduplication by default.
- ACL filtering during index build where practical and mandatory query-time authorization before a lexical or vector candidate is returned.
- Index generation and ACL propagation watermarks.
- Sensitive embeddings and fingerprints encrypted at rest.
- Privacy purge propagation to derivatives.

## 15. Rights and policy model

`RW-MVP-1` performs local exact protection of a source the operator is authorized to read and writes only to the configured exact repository. It may build a local metadata and extracted-text index, but it does not retrieve substitute content, announce or distribute content, seed peers, publish metadata externally, or treat omission as a recoverable representation. The detailed acquisition, redistribution, legal-hold, and privacy-deletion machinery below is required only for a later profile that performs those actions.

Rights evidence may include:

- Publisher-authorized source.
- Public-domain or open-license evidence.
- Organization license.
- Account or purchase entitlement.
- User legal-rights attestation.
- Unknown or disputed status.

Signed rights determinations distinguish `USER_REPRODUCTION_AUTHORITY` from `SOURCE_DISTRIBUTION_AUTHORITY`, reference immutable rights evidence, and separately scope acquisition, transformation, retention, redistribution, public announcement, and peer upload. Possession, matching hashes, purchase evidence without applicable terms, technical access, and operational approval are evidence or execution authority only; they do not establish either rights authority.

Rights are jurisdiction-scoped, time-bound, and separate from fidelity. In a later retrieval or substitution profile, unknown required authority blocks reference-only treatment and triggers exact preservation or promotion of the original while it remains available.

RestoreWeave does not determine legality automatically. Rights evidence and a signed rights determination are separate from operational approval. Policy may block, require review, or allow an action based on configured authority, but no operational approval or break-glass issuance mode creates or replaces legal authority.

Later enterprise profiles represent legal holds as signed immutable resources with scope, authority, jurisdiction, review, expiry or indefinite state, and separately signed release. Holds override retention, garbage collection, source omission, privacy deletion, placement retirement, and crypto-erasure.

Privacy deletion distinguishes catalog and derivative purge, restore suppression, credential removal, crypto-erasure, backend deletion, and copies that cannot be recalled from public swarms or external providers. Recovery and audit records minimize sensitive fields and may use blinded identifiers or signed tombstones where full removal would destroy authenticated-history integrity. Results must never claim external erasure that cannot be verified.

Cryptographic identity, signature validation, path safety, malware quarantine, privacy, residency, legal holds, and other declared non-waivable gates cannot be bypassed by preflight risk acceptance.

## 16. Security logging

Audit events must include actor, request, attempt, source, target, policy, approval, fencing token, and outcome.

For `RW-MVP-1`, logs are local, bounded, and sufficient to reconstruct command, plan, capture, processing, index-generation, initialization, placement, reconciliation, publication, verification, and restore decisions. Logs, the local catalog, and search indexes are evidence or operational projections; none can replace portable signed publication records.

Logs must redact:

- Raw secrets.
- Tracker passkeys.
- Private peer addresses unless explicitly required.
- Sensitive paths and content.
- Model inputs and embeddings by default.

Piece-level torrent telemetry, if a later P2P profile ships it, should be aggregated rather than retained indefinitely.

Later service profiles must declare sensitivity, retention, redaction, aggregation, cardinality limits, access control, and export destinations for metrics, traces, health watermarks, notification events, and anomaly evidence. Telemetry backpressure or loss is itself a durable health event. Health-watermark payloads use pseudonymous monitor identities and exclude paths, filenames, content hashes, repository locators, credentials, and key material.

## 17. Incident response

Required responses:

- Stop the affected operation and prevent new logical publication from untrusted or ambiguous evidence.
- Quarantine the affected source, repository, adapter, Processor, prepared closure, commit marker, or representation.
- Revoke or replace affected credentials and signing identities.
- Preserve audit and forensic evidence.
- Identify affected snapshots and representations.
- Re-run authenticated metadata, content, and clean-machine restore verification as appropriate; never upgrade health from local logs alone.
- In later network profiles, stop network activity and public seeding.
- In later multi-placement profiles, promote qualified unaffected replicas.
- In later custody profiles, rotate keys and rewrap only after proving affected DEKs were not disclosed; otherwise re-encrypt dependent payloads under new DEKs and nonce domains, independently verify replacement placements, and quarantine the compromised generation.
- Revalidate or migrate dependent data and communicate impact and remediation deadlines through the channel qualified by the active profile.

## 18. Security acceptance tests

### 18.1 RW-MVP-1 release tests

1. Install the supported single-node Linux/NAS package and verify `doctor` checks the controller, generic capture implementation, reference repository engine and adapter, RRF reader, baseline index, default processors, workspace permissions, working space, and scoped credential readiness. Failure of an optional platform snapshot helper blocks only that helper profile.
2. Attempt path traversal, final and ancestor symlink swaps, parent and root replacement, bind-mount substitution, mount escape, hard-link confusion, magic-link traversal, reserved-name collisions, and root remapping. Confirm every traversal and read remains component-relative to one retained root anchor, no operation escapes or changes capture basis, and every authoritative observation binds the declared capture or applied-inventory basis.
3. Exercise both a retained immutable capture and a validated live mounted tree. Substitute or reuse stale handles, unmount and remount or replace the source, replace the snapshot, and mutate files during reads. Confirm root, filesystem or volume, mount, snapshot or live basis, resolver profile, and lease or hold evidence are revalidated; snapshot claims require current snapshot evidence, live capture reports its weaker consistency, stable files may remain exact, and unresolved drift blocks the affected claim or requires a new plan.
4. Race regular files with FIFOs, sockets, block and character devices, and ancestor substitutions before open. Confirm type pinning occurs before a potentially blocking content open, special files remain metadata-only under the generic profile, and a missing boundary checker or invalid `CaptureRootBinding` cannot produce an authoritative complete generation.
5. Present unknown, unsupported, malformed, ambiguous, polyglot, encrypted, and processor-failed readable content. Confirm it remains `EXACT_PROTECTED` or `EXACT_FALLBACK`; no detector, processor, search result, MCP client, or model-shaped output can silently exclude it or claim a substitute as exact.
6. Verify suffix evidence is retained before magic-byte evidence, conflicts remain visible, and every file enters either a qualified class route or the generic exact route.
7. Fuzz and resource-exhaust the default metadata and text processors. Confirm sandboxes, cumulative limits, typed partial coverage, and exact fallback prevent escape, unbounded expansion, secret access, or a false complete-processing claim.
8. Delete, corrupt, roll back, or replace the baseline metadata/text index. Confirm search reports stale or incomplete coverage, rebuilds from durable subjects and processor artifacts, resolves every hit through the portable namespace, and cannot change recovery or publication authority.
9. Disable every model and vector component, including the required-by-default local semantic profile. Confirm exact plan, protection, lexical/structured degraded search, verification, browse, recovery-reference export, and restore still work, while status refuses to call the installation a complete default discovery experience.
10. Confirm ingest planning performs no repository write. Between planning and application, create a conflicting repository or unrelated target data and confirm revalidation blocks initialization and placement creation.
11. Interrupt `InitializeTarget` at every supported boundary and exercise `ReconcileTargetInitialization`: matching intent and compatible repository state reconcile; conflicting identity or unrelated data blocks; indeterminate observation remains `RECONCILING` or terminates `UNKNOWN_EXTERNAL_OUTCOME`.
12. Crash or cancel before, during, and after each payload, prepared-closure, and commit-marker side effect. Confirm reconciliation creates at most one logical publication, records equivalent physical engine duplicates without publishing twice, blocks conflicting candidates, and never blindly retries an unknown effect.
13. Stop after payload placement and again after `PREPARED_CLOSURE` but before `PUBLICATION_COMMIT`. Delete the local catalog and index. Confirm neither orphan appears as a published logical snapshot.
14. Create a valid signed `PUBLICATION_COMMIT` and confirm it binds the exact repository, RRF root, payload receipt, prepared-closure receipt, plan digest, capture or applied-inventory digest, authenticated-metadata evidence, and publication generation or fence. Authenticate the commit-marker placement separately.
15. Forge the commit signature, replay a stale generation, substitute the repository, payload, prepared closure, plan, capture basis, or verification evidence, alter private engine metadata, or present a key learned only from the candidate companion. Confirm clean discovery rejects every case.
16. On a clean qualified installation with no original catalog, index, source host, processors, plugin registry, AI service, MCP client, REST service, or WebUI, use only the selected repository, independently retained recovery reference, separately available scoped credential source, independent trust anchor, and supported reader. Discover the valid commit, reconstruct the namespace, restore to an empty destination, and verify every regular-file digest and declared metadata contract.
17. Omit the recovery reference, independent trust anchor, or separately available credential source one at a time. Confirm recovery readiness is degraded or blocked and a credential available only on the original host cannot satisfy clean-install readiness.
18. Corrupt the commit marker, prepared closure, payload, placement receipt, or sampled file content. Confirm authenticated-metadata, sampled-content, full-byte, and restore verification remain distinct and the affected claim fails without an override path.
19. Lose or withhold the sole repository. Confirm RestoreWeave reports loss of recoverability and never reports redundancy, provider independence, a repair source, or survival of repository loss. A same-failure-domain repository remains test-only and visibly at risk.
20. Attempt to make the qualified MCP adapter open a listener, initialize or write a repository, mutate or apply a plan, restore a destination, access a credential or arbitrary live path, install a processor, run a shell command, or invoke arbitrary processing. Confirm every attempt is unavailable or denied.
21. Replay commands and idempotency keys across operators, workspaces, operations, plans, captures, repositories, or attempt fences. Confirm a superseded attempt cannot commit an initialization, placement, restore, index publication, or recovery publication side effect.
22. Restore executable content and confirm no application, game, script, or recovered binary executes automatically. Crash or cancel restoration and confirm the isolated partial destination cannot be reported as verified success.
23. Inspect CLI output, MCP results, search results, events, logs, RRF records, prepared closures, commit markers, and recovery references. Confirm they contain no plaintext repository password, access token, private signing key, reusable secret, or unrestricted path capability.
24. For any RestoreWeave-owned encrypted state, crash after encryption but before checkpoint at every boundary and prove that no key and nonce pair is reused with different plaintext or associated data.

### 18.2 Conditional later-profile tests

The following tests are retained for profiles that ship the corresponding capability. They do not gate `RW-MVP-1` and cannot replace its recovery-reference and `PUBLICATION_COMMIT` clean-restore tests.

1. Fuzz magnets, bencoding, network parsers, deep archives, media parsers, model loaders, and plugin manifests; enforce cumulative CPU, RAM, disk, inode, connection, recursion, and expansion limits.
2. Attempt SSRF to loopback, private ranges, cloud metadata, and DNS-rebinding targets; capture traffic for every network privacy profile.
3. Confirm zero payload-piece and content-derived metadata upload in a qualified download-only P2P profile, reject wrong or malicious torrent content by independent whole-file hash, and isolate DHT, peer, resume, tracker, credential, and NAT state across workspaces and profiles.
4. Attempt AI prompt injection, model-generated policy broadening, semantic-index ACL bypass, stale-vector disclosure, and external-enrichment exfiltration; confirm deterministic policy and query-time authorization prevent side effects or unauthorized results.
5. Attempt cross-tenant deduplication, existence inference, search leakage, token replay, CSRF, CORS, request smuggling, and expired distributed-worker commit.
6. Compromise ordinary write credentials and confirm a separately controlled immutable generation or qualified independent placement survives according to that later profile's stated claim.
7. Run fenced garbage collection concurrently with restore and migration; exercise legal-hold creation, review, release, privacy deletion, crypto-erasure, and honest unrecoverable-public-copy reporting.
8. Roll back or fork a later snapshot-publication projection, Recovery Bootstrap Seed, Recovery Bootstrap Envelope, or RecoveryHeadWitness history and confirm canonical selection blocks. With the original KMS, IdP, DNS, registry, control database, and source host absent, use the separately qualified bootstrap and custody chain to resolve the required ControlPlaneRecoverySet, Capsule Cores, readers, and dependencies from authenticated protected placements.
9. Lose, compromise, duplicate, and replace key shares and custodians; confirm quorum health changes immediately and recovery evidence never exposes secret material.
10. Exercise KEK-only compromise and DEK disclosure separately; permit verified rewrap only for the former and require verified payload re-encryption, new nonce domains and placements, signed migration evidence, and quarantine for the latter.
11. Supply manifest shell commands, interpolation, inherited-environment attacks, mutable executable paths, and undeclared subprocesses; confirm they are rejected and allowlisted native tools remain inside the declared capability sandbox.
12. Inject mass deletion, rename, truncation, encryption-like rewrite, entropy change, and canary modification; confirm the qualified anomaly hold preserves required history and freezes every destructive lifecycle action in scope.
13. Replay, forge, suppress, and stop health watermarks and notification delivery; confirm the independent monitor reports late, invalid, rolled-back, and non-advancing states within the declared SLO.
14. In a later mutable-destination restore profile, exercise overwrite, merge, rename, skip, atomic replacement, rollback, retained preimages, and write-ahead reconciliation. Dynamic executable validation must remain separately authorized and sandboxed.

For `RW-MVP-1`, any silent protection downgrade, implicit omission, unauthorized network listener or destination, out-of-root read or write, stale-attempt commit, untrusted repository initialization, false logical publication, secret disclosure, or false exact-recovery result is a release blocker. Later profiles add release blockers for every authority and claim they introduce.
