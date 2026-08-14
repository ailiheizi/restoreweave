# Peer-to-Peer and Magnet Requirements

> **Profile status:** Magnet retrieval, peer-to-peer transport, seeding, swarm storage, and public discovery are future profiles. They do not affect the initial kernel ABI and are not part of the NAS-first managed-archive MVP, which uses generic capture, exact fallback, baseline local search, and one qualified mature exact repository. Platform- and repository-specific implementations define no P2P requirement or product-wide release gate.

## 1. Purpose

RestoreWeave may use BitTorrent and magnet links through later profiles mapped onto the approved extension seams:

- A BitTorrent `RetrieverDriver` profile may reacquire an externally available pinned artifact.
- A swarm-capable `RepositoryDriver` profile may place RestoreWeave-managed encrypted immutable packs across trusted or approved peers.
- A recovery-artifact package may use a `RepositoryDriver` placement capability for publication and a separate `RetrieverDriver` capability for reacquisition. One package may implement both, but their authority, receipts, and qualification remain independent.

These capabilities share protocol components but have different trust, privacy, legal, availability, and lifecycle requirements.

The governing rule is:

> RestoreWeave owns identity, encryption, policy, and recovery correctness. BitTorrent supplies discovery, distribution, locality, and piece-level repair.

## 2. Non-goals

The integration must not:

- Treat a magnet URI as proof that an artifact is correct or available.
- Treat tracker, DHT, peer, or seed counts as independent backups.
- Automatically search public piracy indexes.
- Bypass DRM, authentication, paywalls, or access controls.
- Seed plaintext private content.
- Make a public swarm the only copy of irreplaceable data.
- Treat a swarm-distributed recovery package or bootstrap artifact as an offline trust root, freshness authority, or sole copy.
- Accept a same-name or semantically similar torrent as exact recovery.
- Describe BitTorrent protocol encryption as payload confidentiality.

## 3. Identity hierarchy

The system must distinguish:

| Identity | Meaning |
| --- | --- |
| RestoreWeave whole-file or object hash | Protocol-independent expected plaintext identity. |
| Signed snapshot and manifest root | Paths, metadata, dependencies, policy, and requested claims. |
| Encrypted pack hash | Exact ciphertext object distributed by a controlled swarm. |
| Torrent v1 or v2 infohash | Identity of the encoded torrent information dictionary and piece layout. |
| Magnet URI | Bootstrap locator carrying exact topics and discovery hints. |
| Tracker, DHT, direct peer, or web seed | A way to locate peers or bytes; not content identity. |

Different torrent metadata can transport the same file or pack and produce different infohashes. Valid torrent pieces prove consistency with that torrent, not that the publisher selected the correct artifact.

Required verification order:

~~~text
magnet topic and metainfo validation
-> torrent piece or Merkle validation
-> complete downloaded file or encrypted-pack hash
-> authenticated decryption where applicable
-> RestoreWeave whole-file or object hash
-> signed snapshot and recovery contract
~~~

## 4. Supported BitTorrent formats

The design must support:

- BEP 3 v1 torrents.
- BEP 52 v2 torrents.
- BEP 52 hybrid torrents.
- BEP 9 metadata exchange.
- BEP 5 DHT when policy permits.
- BEP 12 tracker tiers.
- BEP 17 and BEP 19 web seeds.
- BEP 27 private torrents.
- Optional BEP 46 signed mutable source channels, resolved and frozen to immutable torrent identities.
- BEP 47 file attributes and padding.
- BEP 53 select-only file hints.

The first P2P retrieval phase should prefer v2 or hybrid torrents while retaining an independent RestoreWeave whole-file SHA-256.

## 5. Magnet parsing

Magnet parsing must be offline and cause no DNS, tracker, DHT, peer, or web-seed traffic.

Required parsed fields:

- Redacted display URI and, when policy requires exact retention, an encrypted secret-object reference to the raw URI.
- Repeated exact-topic parameters.
- v1 btih values in hexadecimal or Base32 source encoding.
- v2 btmh multihash values.
- Display name.
- Repeated tracker URLs.
- Repeated `ws` web-seed URLs with magnet provenance distinct from metainfo web seeds.
- Direct peer hints.
- Optional `dht` bootstrap-node hints when that extension is enabled by policy.
- Select-only expression.
- Optional BEP 46 mutable-source fields: `xs=urn:btpk:<public-key>` and salt `s`.
- Unknown parameters.

Rules:

- Preserve the exact raw URI only inside access-controlled encrypted material when it contains credentials or private tracker tokens. Ordinary manifests, APIs, logs, events, prompts, and reports retain a redacted form plus credential references.
- Preserve the parsed representation, including non-secret unknown parameters. Credential-like values are never copied into ordinary unknown-parameter fields.
- Follow RFC 3986 parsing and percent-encoding rules.
- The display name is advisory only.
- Tracker and direct-peer fields are discovery hints.
- Magnet web seeds and DHT bootstrap hints are unauthenticated locators and require the same egress and SSRF policy as locators learned elsewhere.
- A hybrid magnet may contain both v1 and v2 identities only when verified metainfo proves they describe the same hybrid torrent.
- Select-only indices are advisory until authenticated metainfo establishes the file tree.
- A BEP 46 mutable source is not an exact topic. Verify the publisher public key, optional salt, signed value, signature, DHT target, and monotonically increasing sequence before accepting a resolution. Reject rollback, same-sequence conflicts, invalid signatures, target mismatch, and any publisher-key change that is not separately authorized as a new source. Record the authenticated raw signed item and freeze the resolved 20-byte infohash as a new immutable source binding in the snapshot. BEP 46 cannot itself retain a full v2 SHA-256 identity; a later authenticated hybrid metainfo binding may add the independently verified v2 identity without rewriting the frozen resolution.
- Ambiguous, conflicting, or unsupported exact topics are rejected or quarantined.

Hard parser limits must cover URI bytes, parameter count, tracker count, web-seed count, direct-peer count, bootstrap-node count, decoded-string bytes, and Unicode or IDNA normalization. Before allocation, enforce an explicit `max_bep9_info_bytes` against both the BEP 9 handshake `metadata_size` and data-message `total_size`. Maintain separate `max_full_metainfo_bytes` and `max_v2_piece_layers_bytes` limits because BEP 9 transfers only the raw `info` dictionary, not complete root metainfo or v2 piece layers.

A BEP 9 assembly accepts one positive metadata size not exceeding `max_bep9_info_bytes`. The handshake `metadata_size` and every accepted data-message `total_size` must agree. Piece indices are limited to `ceil(total_size / 16384)`; every non-final block is exactly 16 KiB and the final block has the exact remaining length. Reject inconsistent duplicates, overlaps, unsolicited blocks, size changes, out-of-range indices, and trailing bytes before allocation or durable write. Do not interpret paths, attributes, or selections until the complete raw `info` bytes match every pinned exact topic.

Parse BEP 53 `so` without expanding ranges until authenticated metainfo establishes the file count. Bound token, range, and selected-index counts; reject negative, overflowing, descending, malformed, or out-of-range values; normalize duplicates deterministically. A later locator cannot broaden an immutable selection without a new reviewed source binding.

BEP 46 channels require a 32-byte Ed25519 public key, 64-byte signature, salt no longer than 64 bytes, non-negative sequence within the signed 64-bit range, and a signed `v.ih` value of exactly 20 bytes. Each channel declares polling cadence, maximum accepted observation age, and degradation deadline. DHT expiration degrades the mutable locator but never invalidates an already frozen immutable source binding.

## 6. Metainfo retention

A magnet alone is not a durable reference. BEP 9 obtains the information dictionary from peers, but future metadata-serving peers may disappear.

For reference-only treatment, RestoreWeave should retain:

- Original torrent-file bytes.
- Torrent-file SHA-256.
- Exact raw bencoded information-dictionary slice.
- v1 SHA-1 infohash when applicable.
- Full 32-byte v2 SHA-256 infohash when applicable.
- Required v2 piece layers and their digest; a layer is absent or `NOT_REQUIRED` for a file whose length does not exceed the piece length.
- File tree and piece profile.
- Tracker tiers and web seeds as separate locator records.

Original torrent files and magnet URIs may embed private-tracker passkeys or other credentials. Secret-bearing originals must be stored as encrypted, access-controlled objects; portable manifests expose only their authenticated object references, redacted locators, and credential-reference IDs. The raw `info` dictionary and full infohashes remain separate identity material.

The complete original metainfo is preferred. Reacquire-only metadata requires explicit risk approval and successful cold metadata acquisition through independent routes.

Strict bencode validation occurs before interpreting paths or allocating payload space. An invalid decode and re-encode round trip must not silently change the identity.

Reject duplicate or unsorted dictionary keys, non-canonical or overflowing integers, unsupported `meta version`, invalid file-tree leaves, and invalid piece lengths. BEP 52 requires `meta version=2`, a power-of-two piece length of at least 16 KiB and within policy limits, and full 32-byte SHA2-256 identity. Truncated v2 identifiers used by wire protocols or trackers are locator evidence only.

Metadata states distinguish:

- `INFO_DICTIONARY_BOUND`: complete raw `info` bytes match every pinned exact topic.
- `PORTABLE_METAINFO_READY`: the complete metainfo required for offline recovery is retained and authenticated.

BEP 9 alone can establish only `INFO_DICTIONARY_BOUND`. A v2 or hybrid source requiring root-level piece layers cannot become `PORTABLE_METAINFO_READY`, satisfy offline metadata closure, or justify reference-only omission until required full metainfo or piece layers are retained, authenticated, structurally validated, and checked against every selected file's full pieces root.

## 7. File mapping and selection

Every selected file records:

- Raw torrent path components.
- Safe display path.
- Torrent-native index.
- RestoreWeave logical destination.
- Length.
- Selected state.
- Required adjacent files and sidecars.
- Protocol-independent whole-file SHA-256 and optional BLAKE3.

### 7.1 v1

Multi-file v1 torrents form one concatenated byte stream. Pieces may cross file boundaries.

The manifest must preserve:

- Concatenated offset.
- First and last piece.
- Boundary-piece dependencies.
- Padding-file treatment.

A selective restore may need adjacent bytes to verify complete pieces. Unselected bytes are discarded only after verification.

Preflight discloses every adjacent unselected byte range required for v1 boundary-piece verification and its rights and privacy treatment. These bytes remain quarantined and are discarded after verification; they are never committed, indexed, previewed, or exposed as selected content.

### 7.2 v2

Each non-empty file is represented by a SHA-256 Merkle root and is piece-aligned.

The manifest must preserve:

- Full pieces root.
- Piece-layer reference only when file length is greater than piece length; otherwise an explicit `NOT_REQUIRED` state.
- Piece length.
- Complete file length.

When a piece layer is required, validate its expected hash count and tree level, recompute its Merkle root, and compare it with the file's full 32-byte pieces root. The v2 Merkle root remains distinct from the ordinary SHA-256 of the complete file.

### 7.3 hybrid

The client must validate agreement between v1 and v2 names, ordering, padding, piece alignment, and payload mapping before sharing storage.

Any inconsistency places the metainfo in quarantine.

### 7.4 paths and attributes

Torrent paths and BEP 47 attributes are hostile input.

Before download:

- Reject traversal, absolute, drive, UNC, empty, reserved, or overlong components.
- Detect invalid UTF-8, overlong encodings, normalization collisions, and case-fold collisions.
- Reject NUL and unsafe control characters.
- Resolve symlink targets only within the isolated torrent root.
- Treat padding as synthetic data, not user content.
- Detect hidden and executable flags without executing content.
- Build all paths beneath a newly created per-job root using no-follow, descriptor-relative operations.

## 8. Magnet retrieval state machine

~~~text
TORRENT_CANDIDATE
-> NETWORK_APPROVED
-> METADATA_DISCOVERING
-> INFO_DICTIONARY_BOUND
-> PORTABLE_METAINFO_READY where required
-> FILE_TREE_REVIEWED
-> EDITION_MATCHED
-> PAYLOAD_ACQUIRING
-> PAYLOAD_VERIFIED
-> FILE_EXACT or APPROVED_EQUIVALENT
-> PERIODICALLY_REVALIDATED
~~~

Failure and degradation states:

- METADATA_UNAVAILABLE
- NO_PEERS
- INCOMPLETE_SWARM
- SOURCE_DRIFT
- WRONG_EDITION
- MISSING_REQUIRED_COMPONENT
- PATH_UNSAFE
- HASH_MISMATCH
- NO_LONGER_AUTHORIZED
- PRIVACY_POLICY_BLOCKED

A title, filename, size, popularity signal, fingerprint, or embedding remains a candidate only.

## 9. Retrieval workflow

~~~text
offline parse
-> rights, privacy, and network policy gate
-> tracker, DHT, direct-peer, or metainfo-mirror discovery
-> BEP 9 metadata acquisition
-> strict metainfo and infohash validation
-> path, size, file-count, and edition review
-> selected piece plan
-> quarantined acquisition
-> piece or Merkle verification
-> independent whole-file verification
-> exact or approved-substitute validation
-> representation commit
~~~

Payloads are never executed, installed, mounted, imported, or opened by privileged parsers automatically.

## 10. Exact and near-equivalent media

Torrent release identity, selected-file identity, and media equivalence are separate.

### Exact recovery

Original-bit-exact success requires the independent whole-file hash stored before loss.

### Approved substitute

A near-equivalent substitute must:

1. Be acquired while the original or sufficient full-reference material still exists.
2. Match the intended edition, cut, mastering, language, stream, and sidecar profile.
3. Pass the approved component-level fidelity profile.
4. Receive omission approval.
5. Bind the approved candidate's exact whole-file hash, torrent identity, and selected path.

Future restoration retrieves that prevalidated immutable substitute. It does not repeat a semantic guess after the original is gone.

### Composite releases

The system must account for:

- Album ordering, cue sheets, logs, artwork, and mastering.
- Video cuts, audio mixes, subtitles, chapters, fonts, and HDR metadata.
- Image sequences, RAW sources, XMP sidecars, layers, and color profiles.
- Games, DLC, patches, saves, mods, load order, runtime, and signatures.

## 11. Network profiles

| Profile | Behavior |
| --- | --- |
| **OFFLINE_PARSE** | Parse and inspect only; no network traffic. |
| **TRUSTED_OVERLAY** | Authenticated devices over an approved VPN or overlay; public discovery disabled. |
| **PRIVATE_TRACKER** | Announce to one selected allowlisted tracker at a time and connect only to peers returned by it; DHT, PEX, local discovery, `x.pe`, manual/direct hints, and non-tracker resume peers are disabled. |
| **DOWNLOAD_ONLY** | Acquisition over explicitly approved discovery routes with zero payload-piece and content-derived metadata upload technically enforced and inbound listening disabled. Public tracker or DHT announcement additionally requires a current `VERIFIED` signed disclosure determination and `PUBLIC_ANNOUNCEMENT`; without them, the job uses another approved non-announcing route or blocks. Permitted control traffic is disclosed and measured. |
| **PUBLIC_SWARM_SEEDING** | Requires a published SeedPolicy plus distinct current `NETWORK_OPERATION`, `SEED_UPLOAD`, and `PUBLIC_ANNOUNCEMENT` approvals, as well as current `VERIFIED` signed redistribution and public-disclosure determinations. |

`DOWNLOAD_ONLY` is the canonical transfer-profile identifier and composes with an approved route mode such as `TRUSTED_OVERLAY`, `PRIVATE_TRACKER`, or allowed public discovery. A route mode grants no upload authority. The selected route and transfer-profile combination is applied before metadata discovery.

Public discovery mechanisms are controlled independently as `TRACKER_SCRAPE`, `PRIVATE_TRACKER_ANNOUNCE`, `PUBLIC_TRACKER_ANNOUNCE`, `DHT_GET_PEERS`, `DHT_ANNOUNCE`, `WEB_SEED_FETCH`, and `DIRECT_PEER_CONNECT`. A public tracker announce or DHT announce used only to obtain peers still advertises participation and requires a current `VERIFIED` signed disclosure determination plus current `PUBLIC_ANNOUNCEMENT` approval. `NETWORK_OPERATION` authorizes transport only. Without `PUBLIC_ANNOUNCEMENT`, a download-only job with a current signed content-identity-disclosure determination may use approved DHT lookup with `announce_peer` disabled, approved web seeds, approved direct peers, or block; it must not send a public tracker announce or DHT announcement.

Controls include:

- Approved network interface or namespace.
- Tracker, web-seed, and peer destination policy.
- DHT, PEX, local discovery, inbound listening, and port-mapping switches.
- DNS and proxy policy.
- Upload and download limits.
- Log redaction and retention.
- Automatic stop and resume-state behavior.

RestoreWeave must not claim anonymity. Trackers, DHT nodes, peers, DNS operators, and network providers may observe IP addresses and torrent identities.

For a private torrent, tracker failover must disconnect all existing peers before contacting the next allowed tracker. A client that cannot enforce tracker-only peer provenance is rejected for the private profile.

Every P2P job runs in a fresh broker-owned session and network namespace per workspace and immutable network-profile version, or under an equivalently proven state-separation mechanism. DHT routing tables, peer caches, PEX and local-discovery state, tracker state, resume peers, NAT mappings, and credentials must not cross workspace, swarm, or profile boundaries. Source provenance selects the privacy mode before first contact. Private-source jobs use `PRIVATE_TRACKER` from the first packet, with magnet and metainfo web seeds disabled unless individually allowlisted. Discovering `private=1` after prohibited public contact terminates and quarantines the job and emits a privacy-incident event.

Download-only means zero payload-piece upload and zero content-derived metadata upload, not zero outbound control bytes. It may send explicitly approved tracker scrapes, DHT lookups with announcement disabled, handshakes, metadata requests, web-seed requests, and direct-peer requests, but it must not serve the raw information dictionary, piece layers, metadata responses, payload pieces, or derived swarm metadata. RestoreWeave must enforce this through a capable client and the mandatory P2P Network Broker, optionally reinforced by an independent external egress boundary, and verify it through packet capture or equivalent flow evidence. A stock client label or upload-rate setting is insufficient; an implementation that cannot enforce both zero-upload guarantees is blocked, and any profile that permits metadata serving is not `DOWNLOAD_ONLY`.

Content-derived protocol egress includes the raw information dictionary, piece layers, metadata responses, payload pieces, and derived swarm metadata. `NETWORK_OPERATION` permits transport but grants no distribution authority. Metadata egress requires a current `VERIFIED` signed `SOURCE_DISTRIBUTION_AUTHORITY` determination scoped to metadata class, recipients or publicity, territory, and duration; otherwise the broker enforces `NO_METADATA_EGRESS`. Payload-piece upload additionally requires `SEED_UPLOAD`, and public advertising additionally requires `PUBLIC_ANNOUNCEMENT`.

## 12. Tracker and web-seed security

Tracker and web-seed URLs can create SSRF risk.

Requirements:

- Apply explicit scheme, hostname, port, address-range, and redirect policy.
- Reject embedded plaintext credentials and IDNA confusables.
- Resolve and revalidate destinations at connection and redirect time.
- Block loopback, link-local, multicast, unspecified, private, carrier-grade NAT, and cloud-metadata ranges unless a dedicated private-network profile explicitly permits them.
- Run network workers without ambient cloud credentials.
- Limit redirects, response size, time, and retries.
- Extract tracker passkeys into credential references and redact them from logs, manifests, events, and agents.

Web-seed clients request identity encoding, validate `Range` and `Content-Range`, enforce exact response lengths, and reject transparent transformation. Authentication headers, cookies, passkeys, and signed-query credentials do not cross origin or redirect boundaries. Every redirect and connection is re-resolved and re-authorized, and no web-seed byte is committed before torrent-native verification.

Private-torrent flags constrain discovery but do not encrypt content.

## 13. Resource controls

Preflight must show declared total bytes, file count, piece count, selected files, and expected allocation.

Enforce quotas for:

- BEP 9 `info` bytes through an explicit `max_bep9_info_bytes` checked before allocation.
- Complete torrent-file bytes, v2 piece-layer bytes, and bencode depth through separate limits.
- Logical and allocated bytes.
- File and inode counts.
- Path bytes.
- Piece layers and piece counts.
- Peers and connections.
- CPU, RAM, disk, bandwidth, duration, and retries.
- Temporary and partial data.

Allocate lazily inside a quota-controlled volume. Do not automatically extract archives or mount images.

## 14. Swarm storage design

A swarm-capable `RepositoryDriver` publishes encrypted immutable RestoreWeave packs, not raw files or individual content-defined chunks.

Recommended pack size is approximately 64 to 512 MiB, configurable by workload.

~~~text
plaintext files
-> content-defined chunks
-> repository deduplication
-> lossless compression
-> immutable pack assembly
-> framed authenticated encryption
-> ciphertext pack hash
-> v2 or hybrid torrent
~~~

Pack requirements:

- Opaque pack identifier and filename.
- Full ciphertext hash and length.
- Authenticated encrypted index or footer.
- Per-record authentication and an explicit encryption generation.
- Crash-safe nonce derivation from a domain-separated per-generation subkey plus immutable pack ID and record index, or an equivalently durable pre-allocation scheme.
- Repository-scoped key derivation that prevents nonce-domain reuse across packs, migrations, retries, and rekeys.
- Embedded or retained torrent metainfo.
- Alternative web-seed or object-store locations.
- Signed storage and seed receipts.

Do not expose individual CDC chunks to public metadata. Do not use global convergent encryption by default because it leaks content equality.

Packs are append-only and immutable. A new snapshot references existing packs and adds new packs rather than modifying seeded data.

Swarm granularity is policy-bound. One torrent per small pack is discouraged because it creates excessive swarm, tracker, DHT, and metadata overhead; one mutable torrent for a repository is impossible because torrent identity is immutable. The preferred design seals bounded immutable pack cohorts, uses each pack as a v2 file for selective recovery, and records cohort membership in the signed placement ledger. Policy limits cohort bytes, pack count, metainfo size, active torrent count, tracker and DHT load, batching delay, and garbage-collection fanout. A cohort cannot change after publication.

### 14.1 Optional recovery-artifact swarm placement and reacquisition

A `RepositoryDriver` swarm-placement capability may publish, and a separately qualified `RetrieverDriver` may reacquire, immutable `RWPORT-1` packages, Recovery Bootstrap Seeds, Capsule Core bundles, or other already-signed bootstrap artifacts by pinned digest. These are optional locator, placement, and reacquisition capabilities, not sources of signing authority, freshness, recovery truth, or offline closure.

Requirements:

- The canonical recovery artifact is sealed and signed before torrent creation. The torrent metainfo and optional magnet are recorded in a later signed transport-placement record outside the artifact's own digest and must not introduce a backward reference or content-addressing cycle.
- Confidential artifacts use an authenticated encrypted transport object with opaque names; decryption keys remain outside BitTorrent and outside the transport object.
- Verification proceeds through torrent pieces or Merkle proofs, complete ciphertext hash, authenticated decryption when applicable, canonical artifact digest, artifact signatures, and RecoveryHeadWitness or envelope lineage where applicable.
- At least one independently verified non-P2P, offline-capable placement of every required artifact remains mandatory. Swarm availability, an infohash, a magnet, or peer receipts cannot satisfy the bootstrap trust root or count as the sole copy.
- Publishing, announcing, metadata serving, and payload upload use the same separate network, rights, `SEED_UPLOAD`, and `PUBLIC_ANNOUNCEMENT` gates as other swarm operations. Placement and reacquisition are separately permissioned capabilities and cannot reuse one another's authority or repository-pack authority implicitly.

## 15. Trusted swarm policy

For trusted devices, friends-and-family nodes, or organization peers:

- Prefer an authenticated overlay or private tracker.
- Disable DHT, PEX, and local discovery unless explicitly needed.
- Authorize devices separately from BitTorrent peer identity.
- Distribute decryption keys outside the torrent protocol.
- Use opaque names and encrypted indexes.
- Maintain at least two failure-independent complete replicas.
- Add HTTP or object-storage web seeds for intermittently online peers.

Public copies of encrypted packs may remain observable through infohashes, sizes, traffic patterns, and peer IPs.

A critical snapshot, or any snapshot claiming offline recovery, retains a verified non-P2P fallback placement for every required encrypted pack, metainfo object, piece-layer object, encrypted index, and key-bootstrap dependency. Tracker, DHT, peer, browser, or mutable-feed availability cannot satisfy offline recovery closure.

## 16. Seeding policy

A SeedPolicy declares:

- Daily and monthly upload-byte limits.
- Ratio and duration limits.
- Maximum peers, objects, and concurrent uploads.
- Schedule and bandwidth limits.
- Metered-network prohibition.
- Battery, thermal, and idle constraints.
- Egress-cost ceiling.
- Reserved local storage.
- Minimum independent complete seeders.
- Minimum failure domains.
- Lease renewal interval.
- Grace period before fallback.

Budget exhaustion must not silently violate replica policy. The job enters a waiting state or creates a fallback placement.

Seeding is a separate side effect. It requires a published SeedPolicy, a `NETWORK_OPERATION` approval for destinations and protocols, a `SEED_UPLOAD` approval for the named content and budgets, and a `PUBLIC_ANNOUNCEMENT` approval whenever public tracker, DHT, or locator publication occurs. None implies another, and current signed redistribution and disclosure determinations remain separate. Application restart, approval expiry or revocation, determination expiry, revocation, or staleness, or policy drift must stop the lease and must not silently resume seeding.

A private-source policy records ratio, minimum seed time, automation restrictions, account, device, IP, territory, and credential-renewal obligations. If those obligations conflict with download-only behavior, distribution authority, privacy policy, or unattended restore drills, the job selects another authorized route or blocks. It never silently violates tracker terms or enables upload.

## 17. Availability and repair

Swarm health must not be reduced to reported peer count.

Track:

- Metainfo retention and resolvability.
- Tracker reachability.
- Time to first peer and first verified piece.
- Observed piece coverage.
- Signed trusted-seed possession receipts.
- Failure-domain diversity.
- NAT and inbound reachability.
- Sampled readbacks.
- Last complete clean reacquisition.
- Sustained throughput versus RTO.
- Seed-budget headroom.
- Revalidation age.
- Privacy-policy compliance.

Suggested states:

- UNKNOWN
- DISCOVERING
- HEALTHY
- DEGRADED
- AT_RISK
- UNAVAILABLE
- QUARANTINED

BitTorrent piece hashes detect and repair corruption only when a correct piece still exists somewhere. They are not erasure codes.

Every availability state is derived from a versioned policy defining observation window, evidence expiry, minimum complete independently controlled replicas, required failure domains, measured RTO throughput, and maximum clean-readback age. Union piece coverage across multiple peers does not prove that one complete copy exists. Peer counts, tracker claims, DHT observations, and self-asserted receipts cannot independently produce `HEALTHY`.

A trusted-seed possession receipt binds the authenticated device key, operator and failure domains, ciphertext-pack digest and length, torrent identities, verification method, challenge nonce or sample set, complete-check status, observation time, and expiry. Receipts sharing operator, account, site, power, network, or administrative control do not count as independent replicas.

Fast-resume data, cached bitfields, local partial-file state, tracker reports, and peer `have` messages are never verification evidence. After restart, import, worker transfer, or cache uncertainty, re-hash every existing piece before reuse. `FULL_REDOWNLOAD_VERIFIED` and reference-only omission drills start with empty payload and resume caches and recompute torrent-native verification plus every independent whole-file or pack hash.

Replication precedes optional parity. A later implementation may add Reed-Solomon or PAR-style shards over ciphertext packs, with every shard hashed in the signed manifest.

An optional parity generation binds coding algorithm and version, `k/n`, shard length, source ciphertext-pack digests, ordered shard digests, repair threshold, failure-domain placement, verification method, rekey behavior, and full-restore qualification. Parity never replaces the required complete-replica floor until independent failure and repair tests prove the declared objective.

Traffic-analysis controls may use policy-defined pack-size buckets, authenticated padding, batching, and schedule jitter. They reduce some leakage but do not provide anonymity. Stable infohash reuse can reveal ciphertext equality, object-set evolution, and backup cadence; this residual exposure is disclosed before public or shared-swarm publication.

## 18. Rights and entitlement

An infohash, magnet, public swarm, matching digest, technical access, or evidence of prior local possession does not prove current authorization to reproduce an artifact or prove that a source is authorized to distribute it.

Requirements:

- Reacquisition is scoped to a specific artifact and initiated by an authenticated actor under an explicit policy; no ambient title search or automatic source substitution is implied.
- Create separate immutable signed `USER_REPRODUCTION_AUTHORITY` and `SOURCE_DISTRIBUTION_AUTHORITY` determinations referencing exact rights evidence, jurisdiction, time, provider, license or applicable receipt terms, expiry, operation scope, and restrictions.
- If either required authority is unknown, expired, revoked, or conflicting, reference-only omission is blocked and the original bytes are retained or promoted while they still exist.
- Prefer publisher-authorized torrents and mirrors.
- Permit organization source allowlists or complete public-swarm prohibition.
- Block flows that require DRM or access-control circumvention.
- Treat public seeding as a separate redistribution decision.
- Do not bundle a default public title-search or piracy-index integration.
- Treat any future hosted public index or user-submitted magnet service as a separate product surface requiring jurisdiction-specific notice, takedown, repeat-infringer, privacy, and counsel review.

## 19. API and WebUI requirements

Required resources:

- retrieval-sources
- network-profiles
- swarms
- swarm-objects
- peer-nodes
- seed-policies
- seed-leases
- availability-checks

The WebUI must support:

- Offline magnet inspection.
- Tracker, DHT, PEX, web-seed, filename, and IP-exposure preview.
- File and component selection.
- Quarantine and safety warnings.
- Torrent, payload, swarm, and locator identity separation.
- Trusted-node and failure-domain topology.
- Seed-budget monitoring.
- Availability history and last full restore drill.
- Separate `NETWORK_OPERATION`, `SEED_UPLOAD`, and `PUBLIC_ANNOUNCEMENT` approval state.
- Clear notice that public copies cannot be recalled.

The WebUI never instantiates a torrent engine, opens WebSocket tracker or WebRTC connections, participates in ICE, STUN, or TURN, accepts peer traffic, or receives pack decryption keys. Any future browser-facing WebTorrent feature is a northbound presentation client controlling a separately sandboxed worker that implements the applicable `RetrieverDriver` or `RepositoryDriver` capability with explicit browser-origin, cache, quota, tracker, WebRTC, relay, and IP-disclosure controls. Browser storage and browser peer observations never count as durable placement receipts.

AI agents may parse magnets offline, propose policies, and run approved dry-runs. They may not contact public discovery, add trackers, publish infohashes, seed content, expose peer identities, raise budgets, reduce replication, or accept unverified payloads by default.

## 20. Manifest requirements

The manifest must record:

- Redacted and parsed magnet data, plus an encrypted raw-magnet object reference when exact secret-bearing retention is required.
- v1 and v2 exact topics.
- Full retained metainfo and raw information-dictionary digest.
- Required piece layers and explicit `NOT_REQUIRED` states for smaller v2 files.
- File mapping and selected components.
- Independent whole-file hashes.
- Tracker tiers, DHT policy, web seeds, and direct peers separately.
- Exact revisions and canonical digests for `NETWORK_OPERATION`, `SEED_UPLOAD`, and `PUBLIC_ANNOUNCEMENT` approvals when applicable, plus separate rights-authority records.
- Retrieval client and validator identities.
- Last metadata and full-payload acquisition.
- Torrent, payload, swarm, and locator independence groups.
- Availability and seed receipts.
- Optional recovery-artifact transport bindings, including artifact type and canonical digest, signature set, encryption profile, ciphertext hash and length, retained metainfo, external transport-placement record, and independent non-P2P fallback placements.
- Ordered fallback.

## 21. Failure states

Required BitTorrent-specific states include:

- BLOCKED_TORRENT_METADATA_UNAVAILABLE
- BLOCKED_TORRENT_NO_PEERS
- BLOCKED_TORRENT_INCOMPLETE_SWARM
- BLOCKED_PRIVATE_TRACKER_AUTHENTICATION
- BLOCKED_TORRENT_RIGHTS_POLICY
- BLOCKED_TORRENT_PRIVACY_POLICY
- FAILED_TORRENT_INFOHASH_MISMATCH
- FAILED_TORRENT_PIECE_HASH_MISMATCH
- FAILED_TORRENT_MERKLE_PROOF
- FAILED_TORRENT_FILE_MAP_CONFLICT
- FAILED_TORRENT_PATH_SAFETY
- FAILED_TORRENT_FINAL_HASH
- WRONG_TORRENT_EDITION

## 22. P2P delivery sequence

### Phase A: offline and download-only retriever

- Parse magnets offline.
- Support retained metainfo.
- Use v2 or hybrid where possible.
- Resolve metadata under an explicit network profile.
- Download selected content into quarantine.
- Verify full file hashes.
- Enforce zero payload-piece and content-derived metadata upload, with no inbound listening, DHT announcement, PEX, local discovery, port mapping, or automatic resume. Permitted control traffic remains disclosed and policy-enforced; inability to prove both zero-upload guarantees blocks the job.
- Use fresh isolated session state and re-hash all reusable partial data before it contributes to verification.

### Phase B: trusted encrypted swarm

- Publish immutable encrypted packs.
- Use an authenticated overlay or private tracker.
- Maintain multiple complete replicas and web-seed fallback.
- Add seed leases, budgets, possession checks, and clean restores.

### Phase C: advanced repair and public interoperability

- Optional public retrieval profiles.
- Hybrid v1 and v2 support.
- Encrypted-pack public distribution when explicitly authorized.
- Optional parity shards.
- Signed mutable pointers only as convenience locators, never snapshot authority.
- Prefer a RestoreWeave-signed mutable feed for durable full v2 or hybrid update history if measured demand justifies it; do not extend BEP 46 beyond its 20-byte v1 identity boundary.

Public seeding and public DHT-only critical recovery remain outside the first P2P release.

## 23. Release gates

The adversarial test environment must include fake trackers, DHT nodes, peers, web seeds, DNS, cloud-metadata endpoints, and filesystem escape targets.

Release requires:

1. Zero writes outside the job root.
2. Zero requests to blocked network addresses.
3. Bounded CPU, RAM, disk, inode, connection, and bandwidth use.
4. Zero payload-piece and content-derived metadata upload in default retrieval mode, verified by packet capture or equivalent flow evidence. A client that cannot prove both guarantees is blocked.
5. Complete final hash equality for every exact result.
6. No mutable, similar, wrong-edition, or incomplete candidate promoted to exact.
7. No torrent reference counted as protected until a clean full acquisition succeeds.
8. No last healthy replica retired without a verified replacement.
9. Crash and retry injection cannot reuse an AEAD `(key, nonce)` pair for different pack plaintext or associated data.
10. BEP 9 size, block count, block length, duplicate, overlap, and exact-topic assembly rules reject malformed or conflicting peers before durable interpretation.
11. A v2 or hybrid reference requiring piece layers cannot reach portable-metadata readiness without authenticated required layers.
12. Fast-resume, cached bitfields, partial files, and peer claims cannot bypass complete re-hashing.
13. P2P session state, credentials, peers, DHT tables, and NAT mappings cannot cross workspace or network-profile boundaries.
14. Public tracker or DHT announcement cannot occur under transport approval alone, and metadata egress cannot occur without scoped distribution authority.
15. The WebUI emits no torrent, WebSocket-tracker, WebRTC, ICE, STUN, TURN, or peer traffic.
