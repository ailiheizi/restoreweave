# P2P and Magnet Research

## 1. Conclusion

P2P and magnet support has two plausible optional product roles and one useful supplementary distribution role:

- Reacquire an authorized external artifact through a later BitTorrent `RetrieverDriver` profile.
- Replicate RestoreWeave-managed encrypted immutable pack cohorts through a swarm-capable `RepositoryDriver` profile.
- Deliver an encrypted, independently signed portable recovery package or large bootstrap closure through separate `RepositoryDriver` placement and `RetrieverDriver` reacquisition capabilities.

The third role is a transport and mirror, not a recovery trust root. P2P is not a semantic search system, a durability guarantee, or demonstrated user demand.

The useful product boundary is:

~~~text
semantic candidate
-> immutable torrent release
-> selected file or bundle
-> exact or pre-approved equivalent payload
-> periodically revalidated availability
~~~

A magnet can bind immutable torrent metadata, but it does not by itself prove that:

- The selected file is the user's original or intended edition.
- All required sidecars and streams are present.
- Every piece remains available.
- The user is authorized to reproduce the content.
- The source is authorized to distribute it.

The last two authorities are independent. Matching bytes or prior possession proves neither. This is a product and compliance boundary, not legal advice; applicable rules vary by source, content, contract, and jurisdiction.

Protocol and implementation claims below were verified from primary sources as of 2026-08-11.

## 2. Primary protocol evidence

| Reference | Observed guarantee | RestoreWeave boundary |
| --- | --- | --- |
| [BEP 3](https://www.bittorrent.org/beps/bep_0003.html), Final | The v1 infohash is SHA-1 over the raw bencoded `info` dictionary. Pieces have SHA-1 hashes. Multi-file torrents concatenate files into one piece space, and `name` is advisory. The base protocol is designed for downloaders to upload to one another; tracker announces report the infohash, peer ID, listening port, uploaded bytes, downloaded bytes, and bytes left, and tracker responses disclose peer IP addresses and ports. | Preserve the raw `info` bytes. A v1 multi-file torrent lacks a convenient canonical per-file hash because pieces can cross file boundaries; retain an independent whole-file SHA-256/BLAKE3. Normal client behavior has redistribution and privacy effects, so `DOWNLOAD_ONLY` and public-announcement prohibition require technical enforcement rather than an assumed client default. |
| [BEP 5](https://www.bittorrent.org/beps/bep_0005.html), Accepted | DHT `get_peers` finds peers for an already-known infohash. `announce_peer` stores the announcing peer's observed IP address and port under that infohash. | DHT is not title or semantic search. A peer result is temporary location evidence, not proof that all pieces exist. A DHT lookup exposes interest to contacted nodes, while an announcement additionally advertises participation; account for and authorize these as distinct network effects. |
| [BEP 9](https://www.bittorrent.org/beps/bep_0009.html), Accepted | Peers transfer the raw `info` dictionary in 16 KiB blocks so a client can start from a magnet. The protocol defines no total metadata-size ceiling. `xt` identifies the torrent; `dn`, `tr`, and `x.pe` are display or location hints. | BEP 9 does not recover complete original metainfo, root-level trackers or web seeds, or v2 piece layers. Set an explicit pre-allocation `max_bep9_info_bytes`, retain full metainfo separately when available, and do not treat display names or locators as edition identity. |
| [BEP 19](https://www.bittorrent.org/beps/bep_0019.html), Accepted | `url-list` adds HTTP/FTP web seeds outside the `info` dictionary. Retrieved data still has to pass torrent piece hashes, and a mismatching server is discarded. | Web seeds are replaceable locators, not identity. Preserve them separately with last-verification evidence and independence groups. |
| [BEP 27](https://www.bittorrent.org/beps/bep_0027.html), Accepted | `private=1` is inside `info`; compliant clients announce to one tracker at a time and initiate connections only to peers returned by it, not DHT, PEX, local discovery, or direct hints. Existing peers are disconnected before tracker failover. | Private sources depend on tracker availability, account state, passkeys, and ratio policy. Store secret references, enforce tracker-derived peer provenance, and never retain raw passkeys in ordinary manifests or logs. |
| [BEP 44](https://www.bittorrent.org/beps/bep_0044.html), Draft | Mutable DHT items use a 32-byte Ed25519 public key, optional salt, signature, and monotonically increasing sequence number. The safely assumed bencoded value limit is 1,000 bytes. Items may expire after two hours without re-announcement and should be re-announced hourly. | Use a mutable item only as a compact moving locator. Persist the authenticated raw item and an external monotonic sequence floor, reject rollback or equivocation, and retain the resolved immutable target. Its small, actively republished DHT record is neither a portable recovery package nor a durable newest-head witness. |
| [BEP 46](https://www.bittorrent.org/beps/bep_0046.html), Draft | A signed mutable DHT item addressed by public key and optional salt points to a 20-byte infohash. Publishers and consumers periodically republish or poll it. Its magnet form uses `xs=urn:btpk:...`. | This is a moving publisher channel, not exact payload identity. Freeze every resolved infohash in a snapshot. The draft is v1-era and does not directly carry a v2 SHA-256 infohash. Loss of republishing can make the channel disappear. |
| [BEP 47](https://www.bittorrent.org/beps/bep_0047.html), Draft | Defines padding, hidden, executable, and symlink attributes plus an optional per-file SHA-1. The specification calls that SHA-1 a deduplication hint; piece hashes remain canonical. It warns that malicious combinations can be internally inconsistent. | Do not treat the optional file SHA-1 as authoritative. Preserve attributes as untrusted metadata, block unsafe symlinks, recognize padding, and reject inconsistent layouts without damaging existing data. |
| [BEP 52](https://www.bittorrent.org/beps/bep_0052.html), Draft | v2 uses SHA-256 for the infohash, a file tree, and per-file Merkle roots. A piece-layer value outside `info` exists only for a file larger than the piece length and must validate against that file's root. Hybrid torrents carry compatible v1 and v2 structures. | Prefer v2 roots for selected-file binding, but still retain conventional whole-file hashes. Mark smaller-file layers `NOT_REQUIRED`; validate expected layer shape and both sides of hybrid torrents. Draft status does not imply absence of real implementations. |
| [BEP 53](https://www.bittorrent.org/beps/bep_0053.html), Draft | The optional `so` magnet field identifies zero-based file indices or ranges to select after metadata arrives. | `so` is an acquisition instruction, not content identity. Bind the final normalized path, original file index, length, hash, bundle dependencies, and edition evidence after resolving metadata. |

### 2.1 Portable-package and pack-distribution evidence

Three adjacent primary sources materially constrain the proposed storage and recovery-package roles:

| Reference | Primary relation | Observed mechanism | RestoreWeave decision |
| --- | --- | --- | --- |
| [The Update Framework specification](https://theupdateframework.github.io/specification/latest/) | Security boundary | Offline threshold root keys establish trust. Signed target metadata binds target length and hashes; snapshot and timestamp metadata address mix-and-match, rollback, and indefinite-freeze attacks. Mirrors are not trusted for content authenticity, and the threat model explicitly leaves denial of service possible when no good mirror is reachable. | Authenticate a portable recovery package independently of every torrent, tracker, peer, web seed, and mutable locator. The offline Bootstrap Seed and current RecoveryHeadWitness remain authoritative; a swarm can improve delivery but cannot establish the canonical newest head or availability. |
| [RFC 5854 Metalink](https://www.rfc-editor.org/rfc/rfc5854.html) | Combination | A download description can bind size and whole-file or piece hashes while listing multiple HTTP, FTP, and P2P sources, including a BitTorrent metainfo URL. It can also carry file signatures. | The multi-source pattern supports treating a torrent as one transport for the same signed `RWPORT-1` target. Do not adopt Metalink's optional XML-signature behavior as the trust model; use RestoreWeave's mandatory canonical signatures and verification policy. |
| [Restic repository format](https://restic.readthedocs.io/en/latest/100_references.html#repository-format) | Implementation | Repository objects are named by the SHA-256 of their stored bytes, written once, and not modified; prune removes objects. Pack contents and headers are independently encrypted and authenticated, while indexes and snapshots complete the reference graph. Restic code is [BSD-2-Clause](https://github.com/restic/restic/blob/master/LICENSE). | If Restic becomes one release-qualified repository-engine candidate, its pack files are credible controlled-swarm payloads, but torrenting only `data/` packs is not a recoverable repository. Publish sealed object cohorts only after commit, preserve repository config, keys, indexes, snapshots, and reader closure through ordinary protected placements, and coordinate prune with signed placement reachability. |

The combined evidence supports a third *distribution* role, not a third trust system. It also favors a protocol-neutral signed target descriptor so HTTP, object storage, removable media, and BitTorrent can carry the same authenticated package.

## 3. Candidate, edition, and exact-source binding

RestoreWeave should use explicit source states:

~~~text
TORRENT_CANDIDATE
-> INFO_DICTIONARY_BOUND
-> PORTABLE_METAINFO_READY when required
-> EDITION_MATCHED
-> FILE_EXACT | ESSENCE_EXACT | APPROVED_EQUIVALENT
-> DEGRADED_SOURCE
~~~

BEP 9 can establish only `INFO_DICTIONARY_BOUND` because it transfers the raw `info` dictionary. A v2 or hybrid source that requires root-level piece layers reaches `PORTABLE_METAINFO_READY` only after the required full metainfo or piece layers are retained, authenticated, structurally validated, and checked against the selected files' full pieces roots.

Required binding data:

- Raw `.torrent` metainfo and exact encoded `info` bytes. Treat private metainfo containing tracker passkeys as an encrypted secret-bearing artifact, not ordinary catalog metadata.
- v1 and v2 infohashes where present.
- Torrent version and hybrid status.
- Complete original file tree plus safe normalized paths.
- Selected file index, original path, length, and v2 pieces root when present.
- Independent whole-file SHA-256/BLAKE3.
- Required sidecars, attachments, subtitles, fonts, cue sheets, logs, artwork, and checksums.
- Tracker, DHT, peer, and web-seed locators outside payload identity.
- Edition, cut, mastering, language, stream, codec, and fidelity evidence.
- Last complete reacquisition result, not only metadata or peer discovery.
- Authorization and entitlement references without embedded credentials.

Semantic metadata, perceptual hashes, Chromaprint, CLIP/SigLIP, CLAP, and filenames may generate candidates. They cannot promote a source to exact.

Near-equivalent media must be acquired and validated while the original remains available. Once approved, bind the candidate's exact bytes and torrent identity. A previously unseen substitute cannot safely be accepted after the original is gone because full-reference validators such as ViSQOL, SSIM, LPIPS, and VMAF require the reference.

## 4. libtorrent audit

[libtorrent](https://github.com/arvidn/libtorrent) is the strongest retained implementation candidate.

Observed now:

- Its primary license is BSD-3-Clause. The repository [COPYING](https://github.com/arvidn/libtorrent/blob/RC_2_1/COPYING) also carries additional permissive or public-domain notices for bundled components, which must be preserved with the SBOM.
- Official feature documentation lists BEP 5 DHT, BEP 9 magnet metadata, BEP 19 web seeds, BEP 27 private torrents, and BEP 53 select-only magnets.
- The manual documents v1, v2, and hybrid torrents, SHA-1/SHA-256 infohashes, Merkle data, and hybrid inconsistency errors.
- Current `magnet_uri.cpp` parses and emits `btih`, `btmh`, and `so`.
- The DHT API exposes BEP 44 mutable-item get, put, sequence, salt, public-key, and signature primitives.
- The audited magnet parser contains no `btpk` handling. BEP 46 update-feed resolution is therefore application-layer work unless a separate implementation is found and verified.
- Torrent creation APIs expose v1, v2, hybrid, private, padding, executable, hidden, and symlink behavior.
- `torrent_info` preserves the original layout while also exposing sanitized/renamed paths and parser limits for excessive duplicate names and directory depth.
- The manual states that trusted fast-resume data can avoid piece hash checks. Resume state is therefore not restore proof; force a recheck or complete clean retrieval.

Recommendation:

- Use libtorrent behind a process and capability boundary rather than exposing its session directly to policy or agents.
- Persist full resolved metainfo and independent payload hashes.
- Configure strict parsing, file-count, path-depth, connection, bandwidth, and time limits. Set `max_bep9_info_bytes` explicitly instead of inheriting a client default, and use separate full-metainfo and v2 piece-layer limits.
- Treat download-only as zero payload-piece and zero content-derived metadata upload. Permitted control traffic and metadata requests remain separate, but the client and broker must prevent metadata responses and other content-derived egress and verify both zero-upload guarantees through packet capture or equivalent flow evidence; otherwise the job is blocked.
- Treat metadata-only resolution, swarm observation, piece availability, and complete reacquisition as distinct evidence levels.
- Implement BEP 46 only as an optional signed-channel resolver that freezes immutable resolved infohashes.

## 5. Verified implementation candidates

| Candidate | Verified value | Important boundary | License evidence |
| --- | --- | --- | --- |
| [libtorrent](https://github.com/arvidn/libtorrent) | Mature embeddable C++ engine; documented v1/v2/hybrid, magnet, DHT, web-seed, private, file-selection, and torrent-creation support. | Largest native integration and security surface; no observed high-level BEP 46 `btpk` resolver; fast-resume state must not be trusted as verification. | Primary BSD-3-Clause plus bundled-component notices in [COPYING](https://github.com/arvidn/libtorrent/blob/RC_2_1/COPYING) |
| [anacrolix/torrent](https://github.com/anacrolix/torrent) | Go library with DHT, PEX, WebSeeds, BitTorrent v2, streaming interfaces, and pluggable storage backends. | MPL file-level copyleft; BEP 46 and exact `so` behavior require a focused adoption test rather than assumption. | [MPL-2.0](https://github.com/anacrolix/torrent/blob/master/LICENSE) |
| [rqbit/librqbit](https://github.com/ikatson/rqbit) | Rust library/client with HTTP API, magnet resolution, DHT, private torrents, BEP 47, BEP 53, file selection, and streaming. | Its current documented BEP list omits BEP 52 and BEP 46; do not select it for v2-first recovery without verification or added implementation. | [Apache-2.0](https://github.com/ikatson/rqbit/blob/main/LICENSE) |
| [Transmission](https://github.com/transmission/transmission) | Production client and `libtransmission` reference; 4.0 release notes document v2 and hybrid download support. | Application-first and copyleft; 4.0 notes did not yet support creating v2/hybrid torrents. Evaluate current creation behavior separately if needed. | [GPLv2/GPLv3 option per COPYING](https://github.com/transmission/transmission/blob/main/COPYING) |
| [WebTorrent](https://github.com/webtorrent/webtorrent) | Browser and Node streaming, magnets, trackers, web seeds, file selection, and WebRTC peers. | Its current [BEP support matrix](https://github.com/webtorrent/webtorrent/blob/master/docs/bep_support.md) marks BEP 52 and BEP 46 unimplemented; browser mode also lacks DHT and cannot reach ordinary UDP/TCP peers. | [MIT](https://github.com/webtorrent/webtorrent/blob/master/LICENSE) |

The preferred baseline is libtorrent. Anacrolix is the strongest Go alternative. rqbit is attractive for Rust and REST integration only if v2 requirements are deferred or implemented. Transmission and WebTorrent are useful compatibility references rather than default cores.

### 5.1 Adjacent decentralized-storage evidence

Three primary-source checks sharpen the boundary:

- [Tahoe-LAFS](https://tahoe-lafs.readthedocs.io/en/latest/about-tahoe.html) encrypts files, erasure-codes them into more shares than are required for recovery, verifies shares, and distributes them across servers. This supports optional ciphertext parity and possession-challenge research, but it is a storage system rather than a magnet retriever. Its repository offers GPL-2.0-or-later or TGPPL licensing, so its code is not the default integration path.
- [IPFS persistence documentation](https://docs.ipfs.tech/concepts/persistence/) explicitly distinguishes discoverability from persistence: cached content may be garbage-collected, durable content must be pinned, and somebody must continue carrying its storage cost. This confirms that a CID, like an infohash, is not an availability guarantee.
- [Syncthing file versioning documentation](https://docs.syncthing.net/users/versioning.html) states that versioning defaults to no old copies and applies to changes received from other devices rather than local changes. Peer synchronization can be useful transport, but deletion propagation and incomplete local history make it unsuitable as backup proof.

The design implication is not “support every P2P protocol.” It is to keep one protocol-neutral placement and recovery-evidence contract, then qualify optional adapters only when they add measured value.

## 6. Product implications

### 6.1 Separate identity from location

Model at least:

~~~text
payload identity
-> torrent release and selected bundle
-> swarm
-> tracker, DHT, peer, or web-seed locator
~~~

Multiple magnets with the same infohash are one torrent identity. Different infohashes may contain the same selected file but different bundles. Tracker count, magnet count, and peer count are not independent backups.

### 6.2 Preserve composite media

Torrent recovery should default to a bundle, not one attractive filename.

- Music may require cue sheets, rip logs, artwork, playlists, pregaps, edition, and mastering evidence.
- Images may require RAW files, XMP sidecars, color profiles, masks, layers, or sequence membership.
- Video may require the exact cut, audio mixes, subtitle variants, chapters, fonts, HDR metadata, and attachments.
- Games and applications may require publisher signatures, manifests, data, mods, configuration, and saves. Retrieved executables remain quarantined and must never run as part of validation.

### 6.3 Keep semantic discovery separate

DHT lookup requires an infohash. Human-title or semantic discovery needs an authorized catalog, local index, or user-provided source list. RestoreWeave should not ship a default piracy-index integration.

Torrent metadata normally provides names, paths, sizes, and source hints, not decoded multimodal embeddings. Content-level semantic validation requires acquiring the candidate or using a separately trusted catalog.

### 6.4 Revalidate availability honestly

Use distinct states:

- `METADATA_RESOLVED`
- `SWARM_SEEN`
- `ALL_PIECES_OBSERVED`
- `FULL_REDOWNLOAD_VERIFIED`

Only a clean complete acquisition proves current recoverability. Peer counts and sampled pieces remain hints. Private tracker credentials, ratio rules, mutable-feed republishing, and source authorization can fail independently of payload hashes.

### 6.5 Constrain network and legal behavior

BitTorrent peer connections can transfer data in both directions, and DHT clients can announce participation. Every resolver and restore drill must disclose whether it may contact public DHTs, trackers, peers, or web seeds and whether it may upload pieces.

A public tracker announce or DHT announcement advertises participation even when performed only to obtain peers. It requires applicable disclosure rights and a distinct `PUBLIC_ANNOUNCEMENT` approval in addition to transport approval. Content-derived metadata egress requires scoped `SOURCE_DISTRIBUTION_AUTHORITY`; a download-only profile forbids both metadata and payload upload even when such authority exists.

Required controls:

- Explicit opt-in for public P2P networking.
- Organization policies for public DHT, tracker, and upload behavior.
- Encrypted secret references for private tracker credentials.
- Per-source authorization and entitlement records.
- Independent fields for user reproduction authority and source distribution authority.
- No inference that matching bytes, an infohash, ownership of another copy, or prior possession establishes either authority.
- Jurisdiction- and contract-specific review where required; RestoreWeave documentation should state that it is not legal advice.

### 6.6 Use immutable pack cohorts for swarm storage

BitTorrent metadata is immutable. One torrent per small pack creates too many swarms and too much tracker, DHT, and metadata overhead; one appendable repository torrent is impossible without changing its identity.

The preferred later design is:

~~~text
immutable encrypted packs
-> bounded sealed cohort
-> v2 files inside one immutable cohort torrent
-> signed placement-ledger membership
-> snapshot-independent reuse
~~~

This preserves v2 per-file selection while bounding metainfo growth and swarm count. Cohort size, pack count, active-torrent count, tracker/DHT load, batching delay, and GC fanout require measured limits. A conventional non-P2P placement remains mandatory for critical and offline recovery.

Separate optional `RepositoryDriver` placement and `RetrieverDriver` reacquisition capabilities can transport an already-sealed `RWPORT-1` package or signed bootstrap artifact by pinned digest. Its torrent locator is a later external placement record, not part of the artifact being transported, and the swarm is never the offline trust root, freshness authority, or sole copy.

### 6.7 Use a swarm as supplementary portable-recovery distribution

Later swarm placement and reacquisition profiles may be useful for moving a large `RWPORT-1` export, Capsule Core closure, or other immutable bootstrap bundle among approved peers and mirrors:

~~~text
canonical signed recovery package
-> randomized authenticated encrypted container
-> ciphertext hash and length
-> v2 or hybrid torrent plus conventional mirrors
-> independent package-signature and dependency-closure verification
-> offline Bootstrap Seed and RecoveryHeadWitness select the trusted head
~~~

This role should reuse the P2P broker and torrent engine but remain separately permissioned from external artifact retrieval and pack seeding. It distributes a self-contained export; it does not make the swarm, magnet, BEP 46 channel, or package carrier authoritative.

Required boundaries:

- The signed package descriptor binds the `RWPORT-1` profile, canonical record-set root, ciphertext digest and length, encryption profile, complete bootstrap and reader closure, immutable torrent identities, and non-P2P locators.
- Encrypt the deterministic package with randomized authenticated encryption before public or shared-swarm publication. Use an opaque container or opaque encrypted volumes so torrent paths do not disclose the recovery inventory. Keep key-recovery material outside the swarm.
- Retain at least one independently authenticated, offline-readable Bootstrap Seed and current RecoveryHeadWitness plus a verified non-P2P package path. A clean recovery must be able to start without DNS, trackers, DHT, peers, or a P2P client.
- A BEP 44 or BEP 46 item may advertise a newer package only as a convenience locator. Recovery freezes the resolved immutable infohash and accepts it only when the independent witness and signed package lineage authorize that head.
- Package delivery health, newest-head trust, key recoverability, and offline bootstrap closure are separate health dimensions. A healthy swarm cannot repair a stale witness, missing key, invalid signature, or incomplete dependency closure.
- Qualify the P2P and non-P2P routes with separate empty-cache restore drills. The P2P path is supplementary even after it passes.

This is most valuable for large portable exports and reusable Capsule Cores. Torrents add little value for a tiny envelope or witness record, which should remain easy to copy directly onto independent media and conventional stores.

## 7. Concrete requirements

### P0

- Support v1, v2, and hybrid identity without truncating v2 SHA-256 hashes.
- Save raw metainfo under restricted encrypted storage, plus raw `info`, required piece layers, explicit `NOT_REQUIRED` layer states, selected files, and independent whole-file hashes.
- Treat magnets and semantic matches as candidates until metadata, edition, and payload validation complete.
- Prevalidate any near-equivalent substitute before the original is omitted.
- Preserve required bundle members and component-specific fidelity profiles.
- Sandbox parsing and download destinations; reject traversal, unsafe symlinks, collisions, excessive nesting, metadata bombs, and inconsistent pad/hash layouts.
- Never treat fast-resume data, peer count, or DHT presence as restore proof.
- Revalidate through complete reacquisition and promote degraded references to byte backup while local bytes remain.
- Keep raw credentials out of manifests, URLs, logs, prompts, and vector indexes.
- Require explicit network, public-announcement, payload-piece upload, metadata-egress, privacy, and authorization policy. Download-only must technically enforce zero payload and metadata upload or block.

### P1

- Parse BEP 53 selection but rebind it to stable path, file index, length, and payload identity after metadata arrives.
- Support private-torrent tracker adapters with secret-manager references.
- Record locator and swarm independence groups.
- Support optional BEP 46 signed update channels while freezing every resolved immutable torrent identity.
- Provide a metadata-only resolver mode and a separate complete-validation job.
- Expose exact, essence-exact, approved-equivalent, wrong-edition, missing-sidecar, incomplete-swarm, authorization-blocked, and unavailable outcomes.

### P2

- Add separately permissioned `RepositoryDriver` placement and `RetrieverDriver` reacquisition capabilities only after `RWPORT-1`, Bootstrap Seed, RecoveryHeadWitness, and Capsule Core interoperability are qualified without P2P.
- Publish only randomized-authenticated encrypted package containers or opaque encrypted volumes, with a signed descriptor binding both package identity and ciphertext transport identity.
- Require independently verified offline bootstrap material and a conventional non-P2P package placement; never count a magnet, mutable DHT item, tracker, or swarm as the sole copy or trust anchor.
- Run independent empty-cache recovery drills through the P2P route and the fallback route, then measure whether swarm delivery materially improves RTO or egress cost.

## 8. Counterevidence and boundaries

- Public HTTP, package registries, publisher mirrors, or object stores may be simpler, more stable, and less privacy-sensitive for many legal public artifacts.
- Torrent immutability proves a release, not that it is the desired edition or trustworthy publisher output.
- Rare torrents, private trackers, and mutable DHT feeds may be less durable than storing the bytes.
- Complete revalidation consumes bandwidth and may require public-announcement disclosure; only a separately approved non-download-only profile may upload metadata or payload data.
- Private tracker account, ratio, passkey, or rule changes can block recovery despite intact content identity.
- P2P parsing, peer protocols, and retrieved archives add substantial attack surface.
- Torrent-aware recovery may be associated with workflows the target market does not want to expose or manage.
- No retained demand evidence currently shows that magnet integration changes purchase, retention, or migration behavior.

The strongest evidence against prioritizing P2P is that RestoreWeave's core value can be delivered using authenticated HTTP, package registries, Git, object storage, and existing backup repositories. P2P should remain a plugin until authorized-source prevalence and recovery value are measured.

## 9. Open questions

1. How many target users possess authorized torrent or magnet sources that materially reduce backup bytes?
2. Which lawful public catalogs, private trackers, or publisher feeds can be tested without building a general torrent search product?
3. Is public DHT participation or piece upload acceptable for desktop, NAS, and team deployments?
4. Should the first adapter support only metadata resolution and prevalidated exact sources?
5. Are private tracker credentials and ratio obligations compatible with unattended restore drills?
6. Is BEP 46 needed, or should RestoreWeave use its own signed feed that supports v2 identities and durable retention?
7. What complete-retrieval schedule provides meaningful assurance without excessive bandwidth or network exposure?
8. Which sidecars and alternate streams must be automatically grouped for each media collection?
9. Can libtorrent be isolated and updated safely across supported platforms, or is a separate resolver service preferable?
10. What evidence should users provide for reproduction and source-distribution authority, and how should expiration or revocation be represented?
11. Does encrypted `RWPORT-1` or Capsule Core swarm delivery improve measured RTO or egress cost enough to justify a third separately permissioned distribution profile?

## 10. Coverage boundary

This audit used canonical BEP documents, The Update Framework specification, RFC 5854, the Restic repository-format documentation and license, official libtorrent documentation and source, canonical repository files, release notes, and actual license files. It did not cover private tracker rules, proprietary torrent clients, jurisdiction-specific legal analysis, or authenticated torrent-index communities.

Protocol draft status was preserved rather than treated as absence of implementation. Candidate popularity was not used as correctness evidence.
