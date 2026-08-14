# Cold Media and Offline Custody Requirements

> **Profile status:** Physical media, custody, checkout, witness, and destruction workflows are future profiles. They are not part of the NAS-first managed-archive MVP or its minimum clean-machine recovery bundle. The MVP continues to use generic capture, exact fallback, baseline search, and one qualified mature exact repository regardless of whether a later cold-media profile is installed.

## 1. Purpose and boundary

RestoreWeave may later manage tape, optical media, removable disks, and other physically detached media as additional recovery placements. This capability covers writing, sealing, labeling, custody, health monitoring, refresh, import, recovery, and destruction. It does not treat an offline device, a barcode, or physical possession as proof of integrity or recoverability.

Cold-media support is deferred from `RW-MVP-1`. It may first ship in Phase 3 or later under a separately qualified profile such as `RW-COLD-1`. `RW-MVP-1` uses one release-qualified mature exact repository selected by its applicability matrix and makes no cold-media, multi-placement, or physical-custody claim. A later cold-media placement is additive to the ingest profile that explicitly selects it; it does not retroactively turn the one MVP repository into redundant protection.

## 2. Threats and protection claims

The design must address:

- Mislabeling, duplicate labels, wrong-media insertion, and set-member confusion.
- Loss, theft, unauthorized copying, custody gaps, and insider access.
- Bit rot, scratches, tape damage, failed flash cells, unreadable sectors, and latent write failure.
- Missing set members, failed parity, damaged indexes, and incomplete exports.
- Fire, flood, heat, humidity, dust, light, shock, magnetic exposure, and unrecorded environmental excursions.
- Unsupported filesystems, retired formats, unavailable drives, obsolete interfaces, firmware incompatibility, and missing readers.
- Lost keys, co-located keys and media, compromised custodians, and unavailable signing evidence.
- Ransomware, administrative deletion, premature recycling, and destruction that conflicts with a legal hold.

`OFFLINE`, `OFFSITE`, `WORM`, `SEALED`, and `IN_CUSTODY` are separate claims with separate evidence. None implies that another claim is satisfied.

## 3. Carrier, volume, and set identity

RestoreWeave must keep these identities distinct:

- `media_carrier_id`: one physical cartridge, disc, or device.
- `media_volume_id`: one recorded logical volume and format generation on a carrier.
- `media_set_id`: one ordered set of volumes required by an export plan.
- `media_set_generation`: one immutable membership and layout generation.

Reformatting a carrier creates a new volume identity. Replacing a damaged carrier creates a new carrier and volume identity. Set membership, member ordinal, expected volume count, capacity, format, layout digest, and dependency map become immutable when the set is sealed.

A barcode, QR code, vendor serial, filesystem UUID, tape label, human-readable name, or shelf mark is an observed locator, not recovery identity or authenticity. Labels must be unique in their declared namespace, must not be silently reused, and must be reconciled against authenticated on-media identity. Duplicate or conflicting labels quarantine every affected carrier until resolved by a signed event.

## 4. WORM and seal semantics

Every volume must declare one write-control capability with evidence:

- `MUTABLE`
- `SOFTWARE_APPEND_ONLY`
- `HARDWARE_WORM`
- `PHYSICALLY_FINALIZED`

A write-protect switch, finalized optical session, immutable filesystem flag, or vendor WORM claim must not be promoted to a stronger capability than the qualified hardware and media combination actually enforces.

Every completed volume also requires a signed cryptographic seal that binds:

- Carrier, volume, set, generation, and member identities.
- Canonical ordered object or file inventory, byte lengths, digests, and an authenticated inventory root.
- Volume format, block or file layout, continuation and parity dependencies, and required reader profile.
- Writer, drive, firmware, host, software, policy, and verification identities.
- Write completion, readback completion, seal time, and evidence digests.
- Applicable `RWPORT-1`, RRF root, portable `PublicationCommitRecord`, payload receipt, recovery-reference, and profile-specific reader references. A later profile may add other independently qualified recovery artifacts.

A signed set seal commits the ordered member-volume seals and the complete dependency map. A physical tamper seal and its serial may supplement these records but cannot replace cryptographic verification. Any post-seal mutation creates a new volume generation and seal; it must never rewrite the old seal.

## 5. Location, custody, and failure domains

Locations must use stable hierarchical resources such as site, building, room or fire zone, cabinet, shelf, and slot. Sensitive address and shelf details are encrypted and access-controlled; reports may expose only policy-relevant failure-domain labels.

Replica independence must account for site, building hazard zone, cabinet, media batch, drive model, format, reader implementation, operator, courier, key custodian, and administrative authority. Two copies on one shelf, in one fire zone, made by one failing drive, or unlockable only by one unavailable key are not failure-independent.

Every checkout, handoff, courier shipment, receipt, return, relocation, inspection, or destruction requires an append-only custody event containing:

- Carrier and visible-seal identities and observed condition.
- From and to custodian, location, purpose, and expected return or disposition.
- Event time, trusted-time evidence, transfer method, witness when required, and signatures or authenticated acknowledgements.
- Exceptions, photos or inspection artifacts when policy permits, and the next expected event.

A transfer remains `TRANSFER_UNCONFIRMED` until the recipient acknowledges it. A missed reconciliation, broken seal, unexplained location, or custody gap changes protection health to a visible degraded or unknown state; it is not repaired by editing the last-known location.

## 6. Verified export, seal, and eject

Export is a resumable fenced job with the following order:

1. Freeze the exact export plan, set membership, expected objects, layout, policy, and approvals.
2. Reserve and verify unused labels and authenticated carrier identities.
3. Write only through a pinned writer, media, format, and drive profile.
4. Flush application, filesystem, device, and drive caches and finalize the medium where applicable.
5. Perform policy-required full read-after-write verification, including every object digest and set dependency. High-assurance profiles require an independent reader or drive check.
6. Produce the signed volume seals, set seal, placement evidence, and `RWPORT-1` indexes.
7. Unmount, unload, or eject through the qualified hardware path and record device confirmation.
8. Create the initial custody event only after physical label inspection.

Early removal, power loss, drive reset, failed finalization, incomplete readback, or uncertain cache flush leaves the volume `UNVERIFIED` or `QUARANTINED`. Such a volume cannot count toward a protection objective even when all expected files appear in a directory listing.

## 7. Verified import and reconciliation

Import begins read-only and quarantined. Before any object is trusted, RestoreWeave must:

- Compare scanned labels, on-media identity, set membership, and expected custody state.
- Authenticate the volume and set seals and validate rollback or fork evidence.
- Detect unexpected sessions, files, blocks, layout changes, and duplicate identities.
- Verify the full inventory or the explicitly declared sampling profile; sampling cannot establish full-volume health.
- Reconcile the physical placement ledger without rewriting historical manifests or publication records.
- Quarantine executable or otherwise untrusted imported content under the ordinary restore safety rules.

Import never executes on-media software, auto-runs installers, or silently adopts an unknown volume into an existing set.

## 8. Key and authority separation

Media content-encryption keys, key-encryption keys, manifest-signing keys, custody-signing identities, backend credentials, and destruction approvals must be separate authorities.

Required behavior includes:

- Per-set or policy-bounded encryption domains with explicit algorithm and key-generation identity.
- Multiple wrapped recipients or a declared quorum for critical sets.
- Key custody in a different failure domain from the only usable media copy.
- No plaintext recovery key on the same carrier as the protected content.
- Offline-capable key recovery that does not depend on the original control plane, IdP, KMS, DNS, or network.
- Rotation, rewrap, compromise response, escrow inventory, expiry, revocation, and clean restore tests.

A stolen encrypted carrier is still a security incident. Encryption may reduce disclosure risk only after the exact key lineage, algorithm, wrapping state, and possible cached or escrowed copies are assessed.

## 9. Lost, stolen, damaged, and found media

Incidents use explicit states including `MISSING`, `LOST`, `STOLEN`, `DAMAGED`, `TAMPER_SUSPECTED`, and `FOUND_QUARANTINED`. An incident must:

- Degrade the affected placement and recompute objective health immediately.
- Preserve the last authenticated identity, custody trail, content sensitivity, encryption state, and affected manifests.
- Trigger required key, privacy, legal, insurance, notification, and replacement workflows.
- Prevent the affected carrier from satisfying replica or drill coverage.
- Create and verify replacement placement before any surviving copy is retired.

Found media returns through read-only import, tamper inspection, full verification, and custody reconciliation. It never returns directly to healthy service.

## 10. Partial-set recovery

Every set must declare whether all members are required, which objects are independently recoverable, where continuation ranges occur, and whether authenticated parity or another reconstruction code is present. The planner should keep each immutable object on one volume unless an authenticated spanning format and policy explicitly permit otherwise.

Each volume carries a volume-local inventory plus enough authenticated set identity to report what is present and missing without the catalog. Bootstrap and index material required to begin recovery must be duplicated according to the objective rather than stored only on one data volume.

Missing required members produce an explicit partial or blocked result. Recoverable objects may be exported to a separate destination with a missing-object, missing-range, dependency, and achieved-claim report. Parity counts only after a drill reconstructs and independently verifies the original object digests; parity is not silently counted as a complete replica.

## 11. Media health and error trends

Health observations retain raw vendor data and normalized metrics where available, including:

- Corrected and uncorrected errors, retry counts, unreadable blocks, checksum failures, and bad-sector growth.
- Read and seek latency, throughput changes, load or mount failures, tape alerts, cleaning events, and drive diagnostics.
- Media age, write count, read count, duty cycle, manufacture batch, writer and reader identity, and last full verification.
- Sample selection, coverage, environment, tool version, and confidence or unsupported-metric state.

Trend rules are media- and drive-profile specific. Predicted failure is advisory evidence, but a hard integrity failure is authoritative. Successful mounting, label reading, or sampled verification alone cannot claim full recoverability.

## 12. Refresh and repack

Refresh copies an unchanged sealed representation to newly identified media. Repack may change volume layout, set membership, container, encryption generation, or media technology while preserving representation and provenance rules.

Both operations must:

1. Authenticate and fully read the old source or report every unreadable range.
2. Write, flush, read back, verify, and seal the new set.
3. Publish a new signed physical-placement generation with lineage to the old placement.
4. Complete a clean recovery or policy-required sample from the new set.
5. Observe the retention and rollback grace period.
6. Retire or destroy the old set only after objectives, legal holds, and approvals permit it.

Cancellation or failure leaves the old sealed placement authoritative. Historical manifests, publication records, and old seals are never rewritten.

## 13. Reader and format obsolescence

Every media profile pins the drive or reader class, interface, firmware range, filesystem or archive format, block size, software reader, operating environment, and known limitations. Required recovery dependencies include documentation, conformance vectors, drivers where redistribution is permitted, interface adapters, and at least one independently stored compatible reader path in an authenticated portable package.

Critical profiles require a tested alternate compatible reader or a scheduled migration before the only reader becomes unsupported. Qualification must test media written by each supported writer against each claimed reader combination. A retained drive that has not read known-good test media on schedule does not satisfy the reader-availability claim.

## 14. Environmental evidence

Policies may set media-specific storage and transport envelopes for temperature, humidity, light, dust, shock, vibration, water exposure, magnetic fields, and other relevant hazards. Environmental records bind sensor identity, calibration, location, sampling interval, gaps, excursion duration, and custody leg.

An excursion or monitoring gap creates a health event and may require accelerated full verification, refresh, or quarantine. Sensor readings are evidence about conditions; they do not prove content integrity.

## 15. Legal holds and privacy destruction

Legal holds apply to carriers, volumes, sets, keys, placement generations, custody events, and destruction jobs. A hold blocks recycling, overwrite, crypto-erasure, key destruction, and physical destruction until a separately signed release is valid.

Privacy destruction must distinguish:

- Catalog and locator purge.
- Destruction of every applicable wrapped key and escrow copy.
- Media-qualified physical destruction or certified sanitization.
- Destruction of labels and invalidation of carrier and volume state.
- Unreachable, missing, stolen, exported, or third-party copies that cannot be proven destroyed.

Crypto-erasure counts only when the encryption profile remains approved and every usable key copy is proven inaccessible. Physical destruction uses a method qualified for the exact media type and records operator, witness, device or vendor, time, evidence, and certificate. RestoreWeave must not report complete erasure while an affected copy is merely missing or outside verified custody.

## 16. Portable manifests and `RWPORT-1`

Cold-media export uses the normative `RWPORT-1` directory or deterministic-tar profile rather than a private catalog-only layout. Each volume must carry or authenticate:

- Its volume-local inventory, seal, set root, member map, and continuation dependencies.
- Checksums and the minimum reader needed to validate the local inventory.
- The applicable signed publication, candidate manifest, placement checkpoint, and loss report.
- Policy-required RRF root, portable `PublicationCommitRecord`, payload and prepared-closure receipts, recovery reference, compatible reader information, namespace sidecars, independent trust-anchor identity, and wrapped-key recovery instructions without reusable credentials.

Large metadata may be deterministically sharded, but the recovery-discovery path and shard map must be usable without the original database or network. Key-separation rules still apply; portability does not authorize placing the only decryption secret or the sole trust anchor beside the media.

## 17. Drills and protection health

Cold-media drills must periodically exercise the real custody path: locate a policy-selected set, obtain it through ordinary transfer controls, inspect it, recover keys through separate custodians, use a clean environment and qualified reader, verify seals, restore selected or complete content, validate the requested claims, and return or relocate the media with new custody events.

High-assurance drills inject missing members, one unavailable key custodian, one unavailable reader, environmental excursion evidence, damaged blocks, stale location data, and loss of the control database. Results record selection method, scope, alternate-reader use, manual steps, queue and transport time, key-recovery time, achieved RTO, recovered objects, missing objects, and retained evidence.

Inventory reconciliation, barcode scans, or simulated API transitions cannot substitute for a physical clean-recovery drill.

## 18. Later-profile API resources and operations

RW-COLD-1 does not imply that RestoreWeave Core owns REST or a WebUI. If a later remote custody service exposes these projections, resource families may include:

- `/v1/media-carriers`
- `/v1/media-volumes`
- `/v1/media-sets`
- `/v1/media-seals`
- `/v1/media-profiles`
- `/v1/media-locations`
- `/v1/custody-events`
- `/v1/media-health-observations`
- `/v1/media-environment-observations`
- `/v1/media-incidents`
- `/v1/media-refresh-plans`
- `/v1/media-destruction-records`
- `/v1/media-jobs`

Plan, label, write, verify, seal, eject, import, checkout, transfer, receive, reconcile, refresh, retire, and destroy are distinct operations. Every mutation is idempotent, revision-checked, auditable, and bound to physical-worker evidence. Any later API or WebUI must not report physical completion merely because an operator clicked a button; required scans, device acknowledgements, signatures, and witnesses remain outstanding actions.

Detailed locations, custody identities, keys, incident facts, and destruction evidence require separate least-privilege scopes and redacted list representations.

## 19. Phase and release gate

Cold media is `DEFERRED` for `RW-MVP-1` and cannot be used to claim an MVP capability. A future `RW-COLD-1` capability may become an additional placement only after its release matrix pins supported media, writers, readers, formats, operating environments, WORM claims, health metrics, and destruction methods.

Release requires:

- Pull, power-loss, mislabeled-media, duplicate-barcode, corrupt-block, missing-member, stale-custody, and crash-reconciliation tests.
- Full write/read/seal/import/recovery qualification across every claimed writer-reader-media combination.
- A blank-control-plane and no-network recovery from authenticated portable material.
- Key-separation, lost-media, legal-hold, privacy-destruction, and physical-custody drills.
- A measured refresh and reader-obsolescence plan before vendor or format support ends.
- Proof that the selected later multi-placement profile remains healthy without miscounting cold media or the single `RW-MVP-1` repository as redundant.

## 20. Acceptance tests

1. A duplicate or conflicting barcode cannot select or authenticate a volume and quarantines the conflict.
2. Removing or powering off media before flush, finalization, verification, and seal completion cannot create a healthy placement.
3. A modified byte, reordered object, wrong set member, stale seal, or unexpected post-seal session fails import verification.
4. The system never claims hardware WORM without qualified enforcement evidence for the exact media and writer profile.
5. A lost, stolen, damaged, or custody-unknown carrier immediately degrades every dependent objective and triggers an incident workflow.
6. A found carrier remains quarantined until identity, full integrity, tamper, and custody checks pass.
7. Recovery with one missing member reports exact recoverable and missing objects; full success requires verified reconstruction or another complete placement.
8. Rising error trends or an environmental excursion trigger the configured verify, quarantine, or refresh action without being presented as proven corruption.
9. Refresh or repack failure leaves the old sealed placement authoritative; retirement occurs only after new-placement verification and grace.
10. A clean recovery succeeds with the original control database and network unavailable, using `RWPORT-1`, its committed RRF recovery closure, a scoped credential source, and an independent trust anchor.
11. Loss of the primary reader is covered by a tested alternate reader or produces a visible objective failure before the migration deadline.
12. Decryption fails when only the media is present, while the documented independent custodian procedure can recover the required keys.
13. A legal hold blocks key destruction, sanitization, recycling, and physical destruction.
14. Privacy erasure cannot report completion for a missing, stolen, or otherwise unverified copy.
15. A cold-media placement is never reported as part of `RW-MVP-1` and never causes its one qualified exact repository to be described as redundant.
