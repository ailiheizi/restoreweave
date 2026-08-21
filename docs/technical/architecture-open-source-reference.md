# Whole-Architecture Open-Source Reference

> **Status:** Informative implementation discipline, recorded 2026-08-14 and adversarially re-reviewed the same day. This document does not add product requirements, select a release repository engine, or adopt a WebUI, player, or reader into `RW-MVP-1`. Component pins and qualification gates stay in [Open-Source Adoption and Code Borrowing](../references/open-source-adoption-and-code-borrowing.md). Domain-client boundaries stay in [Ecosystem Application and Adapter References](../references/ecosystem-application-adapters.md) and [Ecosystem and Vertical Applications](../requirements/ecosystem-and-vertical-apps.md).

The purpose of this note is to keep later work from optimizing one slice in a way that blocks a cheaper open-source introduction later. Section 8 records the four-perspective review that reversed the “mount first, then point clients at it” default. Closed decisions and the remaining list live in [Remaining Work and Closed Decisions](remaining-work-and-closed-decisions.md).

## 1. Decision

Write the authority-owning core. Consume mature open-source software for parsers, repository engines, protocol stacks, and later experience UIs. Do not embed those UIs or media servers into `restoreweaved`.

Keep two proofs separate. The **recovery proof** is authenticated clean restore. The **product proof** is ingest -> fused search -> saved view -> frozen export -> materialize/verify, without internal IDs. Neither waits for a WebUI, facade, live FUSE mount, or media server.

Introduce experience software as **separate processes** that read a RestoreWeave-owned surface. The file-shaped export and the annotation-capable surface are not the same thing:

```text
Core command ABI and FileAccess
  -> thin Inbox shell over that ABI (product proof; not an RW-MVP-1 restore dependency)
  -> optional in-page FileAccess preview on the item page (not a media server)
  -> OpenSubsonic / OPDS facades when progress must return to SubjectRef
  -> existing clients as interoperability demos
  -> file-shaped bytes via restore or FileAccess; other tools may mount that
```

RestoreWeave does not own a kernel filesystem adapter. `plan.restore` writes a real directory; export materialization, `content.open`/`read`, and `FileAccess` hand out exact bytes. External tools may mount or share those outputs. That work stays outside `restoreweaved`, and no mount code or dependency remains in tree.

Progress, bookmarks, tags, and notes remain RestoreWeave annotations bound to `SubjectRef` / `SegmentRef`. A client-private database is not recovery authority. Pointing Navidrome, Komga, or Jellyfin at a mount demonstrates that a snapshot is readable. It does not demonstrate a unified catalog.

Stars, GitHub topics, and popular framework names are discovery signals for **which sidecars to test**. They are not adoption reasons and they do not justify linking a GPL/AGPL product into the core. Not checking NAS package catalogs or awesome-selfhosted is a coverage gap, not rigor.

A process or container boundary is an engineering control, not a licensing or confidentiality safe harbor ([adoption §2 and §9.6–9.7](../references/open-source-adoption-and-code-borrowing.md#9-license-and-supply-chain-policy)).

## 2. What this checkout actually has

Observed on 2026-08-14 in this tree:

- Direct `go.mod` dependencies currently include `modernc.org/sqlite`, `google.golang.org/grpc`, `modelcontextprotocol/go-sdk`, `spf13/cobra`, `golang.org/x/sys`, and `klauspost/compress` for the local-zstd candidate; the target local semantic profile later adds pinned ONNX and zvec packaging.
- One embedded Inbox page (`server/internal/gateway/protocol/inbox.html`). No Vue/React app, no File Browser, no player product.
- In-process EXTRACT for UTF-8 text, ID3/FLAC/OGG tags, and EPUB OPF. Exact ingest ignores processor failure.
- The in-tree `RepositoryDriver` has a raw development profile and an embedded local-zstd measurement candidate. Darwin/Unix CLI probes for Restic and Kopia can pass when those binaries are present; none of these facts is an engine selection.
- Linux bubblewrap execution remains open where isolated heavy parsers are wanted. Kernel mounting is not a RestoreWeave product surface.
- An optional loopback OpenSubsonic/OPDS/Inbox facade can bind `127.0.0.1` and call the command ABI. It is not a player and not enabled unless `--facade-listen` is set.

Tika, libarchive, ffprobe, libmagic, Siegfried, pathrs-lite, and a release repository engine are planned or qualified elsewhere. They are not present as Go module dependencies today.

## 3. Introduction modes

Use the least-coupled mode that still preserves recovery meaning. Do not collapse these into “we use open source.”

| Mode | When to use | Examples |
| --- | --- | --- |
| **Host-owned core** | Identity, admission, snapshots, verification, restore, command ABI | RestoreWeave records and `FileAccess` |
| **Pinned library, types private** | Narrow adapter whose types must not enter portable records | SQLite FTS5, zvec, ONNX Runtime binding, Cobra, MCP SDK, grpc-go |
| **Isolated sidecar** | Parsers and probes that must not hold ambient authority | Tika, libarchive, ffprobe after bubblewrap |
| **CLI / process driver** | Engines whose supported surface is a product CLI | Restic after remaining Driver gates. Kopia qualification currently uses the binary; the adoption note still prefers the Go repository API if those gates pass. Do not treat “process boundary” as the Kopia decision. |
| **Foreign tools** | Operator wants a folder or protocol view | Restore or export a snapshot (or read via `FileAccess`) and let external tools provide any mount/share behavior. Do not grow a RestoreWeave mount daemon. |
| **Protocol facade** | Progress, bookmarks, and library identity must return to `SubjectRef` | Narrow OpenSubsonic / OPDS adapters. Still not a player. |
| **Thin command-ABI shell** | Universal Inbox browse/search/restore as the `RW-CATALOG-1` visible proof | A small read-only WebUI over commands; File Browser patterns only, never its writable job |
| **Design reference only** | Useful UX or routing lessons, incompatible license or job | sist2, Paperless-ngx, Immich, Lutris. This is not a license to copy UI, CSS, icons, or a recognizable flow. |

GPL/AGPL projects may run beside RestoreWeave. Linking them into a permissively licensed core is a separate licensing decision and is not implied by “reference.” Shipping them in the same compose file, NAS package, or installer is also a separate decision: optional containers still need corresponding source for those works, and a shared product name, unified EULA, private glue protocol, or patched binary can stop looking like mere aggregation. AGPL works (Immich, KOReader) add a network-user source obligation on modifications even when they are not linked.

## 4. Layer map and retrofit traps

| Layer | Current / planned open source | How to introduce | Local-optima trap |
| --- | --- | --- | --- |
| Capture | pathrs-lite; fsnotify as hint only; official ZFS/Btrfs CLIs | `CaptureDriver` after Linux handle qualification | Treat watcher events as authoritative deletes |
| Identification | Host suffix table; planned libmagic; optional Siegfried | Evidence only; never exact identity | Keep growing a hand-written format table as identity |
| Repository | Raw development CAS; embedded local-zstd candidate; Kopia/Restic qualification | Measure local zstd, then qualify a mature release adapter after GC-root, crash, corruption, migration, reader, and applicable NAS/S3 gates | Serialize backend object types into portable records or call the zstd candidate production |
| Processor | In-process text/audio/EPUB extracts | Sandboxed Tika / libarchive / ffprobe after bubblewrap | Add more in-process parsers instead of the sidecar pack |
| Search | SQLite FTS5 plus in-process zvec generations | Keep both schemas private; later Qdrant/Milvus only as separately qualified service profiles | Publish index tables or vector rows as ABI |
| Namespace / file egress | `SnapshotTree` / `FileAccess` / `plan.restore` / `ExportManifest` | Keep exact bytes and namespace facts. Let foreign tools consume a restored tree, export, or read handle. | Own a mount service; leak backend types; treat a foreign client scan as catalog proof |
| Control plane | Cobra, read-only MCP, private gRPC | Stabilize the command ABI before any WebUI | Use MCP as an internal bus or mutation surface |
| Inbox / generic UI | Optional shell in tree, maintenance-only | Keep as a disposable adapter over the command ABI after core gates. Do not use it as release completion | Adopt File Browser as a writable manager, grow a second catalog, or prioritize shell features over the core loop |
| Mixed indexer analogue | sist2 | External Processor or design reference | Let a second index become recovery authority |
| Music | Navidrome and Subsonic clients | Prefer an OpenSubsonic facade or in-page `FileAccess` preview. | Embed a media server or library scanner in `restoreweaved`; treat Navidrome’s database as portable progress |
| Books / comics | Komga (MIT), Kavita/calibre (GPL) | Prefer OPDS. Disable Komga options that rewrite the library (extension repair, convert-to-CBZ, hardlink import). | Make a custom renderer the identity of a book |
| Video | Jellyfin server and jellyfin-web | Later than music/books. Keep NFO-next-to-file and adjacent artwork off; transcode only in a qualified sandbox. | Let Jellyfin own storage, exact bytes, or the daily catalog |
| Photos | Immich | Do not embed; at most consume an export/mount | Create a second photo catalog and identity |
| Apps / games | Lutris, RomM, Pegasus (see adapter catalog) | Read-only inventory first | Become a launcher or downloader |

File Browser is Apache-2.0 and is a legitimate **browse-shell** reference. Its published job includes upload, delete, preview, and edit. That job is not the `RW-MVP-1` Inbox. Borrowing its Vue shell is still an Apache-2.0 derivative and still binds to writable resource APIs unless those controls are removed. Do not take the whole repository (second auth stack, share links, command-runner/hooks). If a later UI borrows the shell, bind it to RestoreWeave commands, keep source-tree mutation off, and test the constraint.

Paperless-ngx is a document-management Inbox, not a heterogeneous recovery plane. Use it for triage UX lessons. Do not absorb it. “Design reference” does not license copying its UI, component tree, icons, or recognizable flow.

## 5. Introduction order

The first qualified **recovery** clients remain the CLI and local read-only MCP. A prototype WebUI must not become an implicit `RW-MVP-1` restore dependency. That sentence does not postpone the Inbox shell until FUSE works.

Keep engineering gates and product proof on separate tracks.

**Engineering (unchanged completion-plan work):**

1. Finish remaining repository gates. Independent SHA-256 readback now passes for raw and local-zstd profiles; the release adapter still needs GC-root, crash, corruption, migration, reader-closure, credential, and applicable NAS/S3 evidence. Isolated processors may use Linux bubblewrap later. Do not spend further work making RestoreWeave a FUSE server.
2. Move general parsing to isolated Tika / libarchive / ffprobe. Stop expanding the in-process tag/OPF extractors. Bubblewrap failure is one reason to keep a processor out of the default pack; missing NOTICE/SBOM, a GPL FFmpeg build, or an unreconciliation of ExifTool terms is another.

**Product proof (may start once the command ABI exists; must not wait for `/dev/fuse`):**

3. Add a thin read-only Inbox shell: triage, search, item detail, verify, restore. It calls the command ABI. It does not open repository packs or SQLite files. This is the `RW-CATALOG-1` visible acceptance, not a later optional adapter.
4. The same item page may play or read through an existing `FileAccess` / `content.open` handle and write progress as an annotation. That is catalog proof, not `RW-AUDIO-1` and not a media server.
5. If a specialized client must return play/read progress to `SubjectRef`, add a narrow OpenSubsonic or OPDS adapter before pointing that client at a mount. `PROGRESS` annotations exist; the remaining work is client-viable methods and live qualification, recorded in [Experience Completion Plan](experience-completion-plan.md).
6. If an operator wants a folder, restore or export the snapshot (or expose `FileAccess` bytes) and let an existing external tool attach it. Do not qualify RestoreWeave by pointing a foreign catalog at an internal service. Jellyfin stays later than music/books.

Do not start a full custom music, reader, or photo application in this repository to “have a frontend.” Use a protocol facade or an external tool over an explicit export; RestoreWeave does not manage mount principals or kernel permissions.

## 6. What would reverse a row

- External directory consumers are **expected** not to carry play/read progress back to `SubjectRef`. Add a protocol facade or write progress from the Inbox item page. Do not implement a media server to close the gap.
- If bubblewrap cannot isolate Tika/ffprobe, or the default pack cannot ship a complete NOTICE/SBOM and an LGPL-only FFmpeg, keep those processors out of the default pack. Do not fold them into the host process.
- If File Browser or another Apache-2.0 shell can be constrained to RestoreWeave commands without writable source semantics, it may be reused as a shell. The constraint must be tested, not assumed. Whole-repo adoption remains refused.
- GitHub topic or star counts will not reverse a license, authority, or writable-source decision. They may change which sidecar is tested first. A NAS-catalog or awesome-selfhosted check that shows a different installed-client set would change the facade order, not the core.
- If the Kopia Go repository API fails GC, crash, or catalog-independent SHA-256 readback, keep the CLI driver. That is a gate failure, not a slogan that “engines are always processes.”

## 7. Evidence boundary

Frontend and experience rows re-checked through GitHub repository search on 2026-08-14 (`sist2`, `navidrome`, `filebrowser`, `komga`, `paperless-ngx`, `jellyfin`, `immich`). Star counts change and are not pins.

The same-day adversarial review additionally checked Navidrome’s published `MusicFolder` read-only option, Komga’s library-rewrite switches, Jellyfin’s config/cache/transcode split, this tree’s `fillAttr` and `AnnotationKind`, and adoption §2 / §9.6–9.7. It did not run a live Linux mount, did not verify awesome-selfhosted or NAS-package catalogs, and did not search Chinese NAS forums. Absence from this search is not an ecosystem-wide absence claim. License notes here are not legal advice.

Related documents:

- [Open-Source Adoption and Code Borrowing](../references/open-source-adoption-and-code-borrowing.md) — pins, licenses, and Driver/Processor gates.
- [Borrowed Projects Catalog](../references/borrowed-projects-catalog.md) — lookup table for those pins.
- [Ecosystem Application and Adapter References](../references/ecosystem-application-adapters.md) — domain clients and protocol facades.
- [Implementation Completion Plan](implementation-completion-plan.md) — remaining Darwin/Unix versus Linux-kernel work.

## 8. Adversarial review (2026-08-14)

Four independent read-only reviews challenged the first draft of this note: counterexample strategy, FUSE feasibility, license/confidentiality, and operator/product. They agreed on the authority boundary and disagreed with the default experience path.

**Held.** Do not embed a player, reader, or media server in `restoreweaved`. A client-private database is not recovery authority. File Browser must not own writable source-tree semantics. Stars do not select Kopia and do not authorize GPL linking. The core must restore without a WebUI. The local-zstd candidate does not waive release Driver gates. Tika/ffprobe stay isolated.

**Reversed or tightened.**

- “Point existing clients at an internal filesystem adapter” is not the default introduction path. External clients consume protocol facades, exports, or restored directories. None of them becomes recovery authority.
- A process/mount boundary is not a license or confidentiality safe harbor. Optional GPL/AGPL containers in the same distribution still need corresponding source. Clients will copy paths, covers, and caches into their own stores or `/tmp`, and they may scrape metadata off-host. `ro` only stops writes to the snapshot.
- “Do not implement a player” forbids a media server, not an item-page `FileAccess` preview whose progress is an annotation.
- “Kopia is a process driver” is the current qualification method, not the adoption decision. The adoption note still prefers the Go repository API if gates pass.
- Stars may inform which sidecar to test. Not measuring installed-client sets leaves the “use what operators already run” claim unfalsifiable.

**Product anti-bias.** Adjacent adoption (Navidrome, Jellyfin, Kopia, Immich, sist2) is counterevidence as well as a consumption list: the target operator may already have a working platter. If weekly listen/watch/read/restore still happens in three foreign UIs, the shared layer has failed the metric in [Ecosystem and Vertical Applications](../requirements/ecosystem-and-vertical-apps.md) §10. Extra processes, extra SQLite, extra library rescans, and a retained source copy all count as operating cost. Interoperability and product proof are different demos.

**Short warnings that stay with this note.**

1. Distribution composition is not a linking exemption. Same installer/compose/NAS suite still carries source obligations for those GPL/AGPL works; shared branding or private glue can exit mere aggregation. AGPL adds a network-user source duty on modifications.
2. A read-only mount used as a library root is not a confidentiality boundary. Do not turn on `allow_other` to make a different-UID container work.
3. File Browser’s Vue shell is an Apache-2.0 derivative bound to writable APIs. Whole-repo reuse is refused.
4. Tika/FFmpeg/ExifTool default-pack cost is NOTICE, SBOM, JRE, and possible CDDL/GPL flags, not only sandboxing. Distro `ffmpeg` is commonly `--enable-gpl`.
5. “Design reference only” does not license copying UI, component trees, icons, or recognizable flows.
