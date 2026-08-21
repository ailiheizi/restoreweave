# Index Dimension Plan

> **Status:** Archived fixture-contract plan, reclassified 2026-08-19. I1–I9 prove only named-dimension and catalog-graph interface shapes. They do not implement the required local ONNX/BGE + zvec default, complete search fields, or the user discovery loop and receive no core phase-completion credit. Authority and current sequencing stay in [Content Store, Views, and Export Requirements](../requirements/content-store-views-and-exports.md) and [Core MVP Execution Plan](core-mvp-execution-plan.md).

The job is to keep the **old client standards** and make the **index layer** a named, rebuildable, multi-dimension projection that later work can extend without becoming a second recovery authority.

## 1. What fixture evidence means

Two proofs stay separate. A third proof is added for discovery, and it is never allowed to impersonate the first.

| Proof | Complete when | Not complete when |
| --- | --- | --- |
| **Recovery** | Ingest → verify → restore matches SHA-256 | A search hit, fingerprint, or embedding is confident |
| **Interop** | OpenSubsonic `/rest/*`, OPDS `/opds`, and Inbox `/inbox/api/*` still speak the old standards and resolve to `SubjectRef` | A new public REST catalog, a Unix-socket SDK, or an embedded player |
| **Discovery** | Every live query names one dimension, one `QueryProvider`, and one `IndexGenerationRef`; deleting the index files leaves exact bytes and annotations intact | “We can Shazam” or “we have vectors” without a provider, a generation, or an honest `UNAVAILABLE` |

A phase is done only when the listed exit test is green. “Sounds like modern search” is not an exit.

## 2. Frozen boundary

**Keep.** `search.query` stays the typed discovery operation. SQLite FTS5 remains the current lexical foundation and every index remains disposable. Existing OpenSubsonic, OPDS, and Inbox adapters are maintenance-only consumers, not public-core or completion requirements. Processors may fail; they must not block exact ingest / verify / restore.

**Own.** Dimension identity, generation identity, query-provider compatibility, result resolution to `SubjectRef`, construct-axis names, and the rule that a missing later dimension is `UNAVAILABLE` rather than a fake hit.

**Do not own in this fixture plan.** The required local text embedding and zvec runtime must be implemented and qualified under Core Phase 4, not by extending I1–I9 fixtures. Chromaprint/Shazam services, CLIP, alternate embedding spaces, graph stores, or a RestoreWeave player remain later processors or foreign tools. Their features, if added, attach to the same `SubjectRef` and never become `ContentIdentity`.

**Occam.** Do not add a fourth public catalog. Do not add an `rw identify-song` product. Do not put ffmpeg/Tika on the identity path. Do not select or promote a release repository in this plan. Do not expand `RW-MVP-1`.

## 3. Current checkout (generation 0)

Present before this plan:

- One disposable FTS5 database per `IndexGenerationRef` (`server/internal/search`).
- Documents already carry path, name, suffix, entry type, content id, tags, notes, and extracted text.
- `search.query` authorizes hits against the catalog and degrades when the generation file is gone.
- OpenSubsonic `search2` / `search3` and Inbox search call `search.query`. They do not keep a second library.

Missing, and what this plan names:

- The lexical engine was an implicit singleton, not a declared dimension.
- Later acoustic / semantic / multimodal / graph work had no honest capability row.
- A query could not say which construct axes it used or which axes a hit matched.
- Asking for “identify this clip” would have had to pretend or 404 without a contract.

## 4. Dimensions

A **dimension** is a named retrieval space over the same subjects. One `QueryProvider` invocation queries exactly one dimension and exactly one `IndexGenerationRef`. A host-owned broker may later fuse several typed results; fusion is I7, not a hidden lexical rewrite.

| ID | Role | This checkout | Later provider |
| --- | --- | --- | --- |
| `lexical-metadata-fts` | Path, name, suffix, tags, notes, extracted text | **AVAILABLE** when the exact lane built an FTS5 generation | Bundled FTS5 (`query.lexical.fts5.v1`) |
| `acoustic-fingerprint` | Listen / identify / similar-audio candidates | **UNAVAILABLE** by default; fixture exact-lookup exists only with `EnableFixtureDimensions` in a qualification harness | Later isolated Chromaprint-class processor; lookup is not SHA-256 |
| `semantic-embedding` | Meaning over extracted or transcribed text | **UNAVAILABLE in the current normal checkout** with `SEMANTIC_INDEX_UNAVAILABLE`; fixture exact-lookup is harness-only | Required Core Phase 4 pinned ONNX/BGE processor plus zvec generation |
| `multimodal-clip` | Image–text / cover–title joint space | **UNAVAILABLE** by default; fixture exact-lookup is harness-only | Later isolated CLIP-class provider |
| `graph-relation` | Artist / work / package / collection edges | **AVAILABLE** as a disposable catalog projection when the exact lane is wired | Later graph store is optional; this checkout projects existing facts |

Construct axes on the live lexical dimension (I4):

`path` · `name` · `suffix` · `tags` · `notes` · `extracted`

These are fields of one FTS5 generation, not six products. A query may restrict to a subset. A hit reports which axes contained a query token. Axes are not recovery evidence.

## 5. Long sequence

```text
I1  Named dimension registry + capability.list     fixture-contract-complete
I2  search.query provenance                        fixture-contract-complete
I3  Honest UNAVAILABLE for later dimensions        fixture-contract-complete
I4  Lexical construct axes                         fixture-contract-complete
I5  Isolated acoustic fingerprint processor        fixture-contract-complete; opt-in
I6  Semantic / multimodal providers                fixture-contract-complete; opt-in
I7  Host-owned fusion broker                       fixture-contract-complete
I8  Facade mapping for identify-by-sample          adapter-harness-complete
I9  graph-relation catalog projection              fixture-contract-complete
```

Old-standard facades keep working through I8. They consume projections. They never become the index.

### I1 — Named dimension registry + `capability.list`

Status (2026-08-15): **fixture-contract-complete**.

`server/internal/search` declares every dimension in the table above. `capability.list` emits `kind=index-dimension` rows. Lexical is `AVAILABLE` only when this build has a search indexer. Later dimensions are declared and `UNAVAILABLE`, with notes that name the missing isolated processor.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Dimension IDs | Stable strings, not SQLite table names | `capability.list` contains all five IDs |
| Lexical state | Follows whether `search.Indexer` is wired | Exact-lane daemon: lexical `AVAILABLE`; catalog-only daemon: lexical `UNAVAILABLE` |
| Later state | Declared, not implemented | Acoustic/semantic/multimodal are `UNAVAILABLE`, never silent omissions; the catalog-derived graph may be available because it needs no fixture model |

### I2 — `search.query` provenance

Status (2026-08-15): **fixture-contract-complete**.

Every successful or degraded lexical result names `dimension`, `query_provider_ref`, `index_generation_ref`, `score_semantics`, and the construct axes in force. Hits still resolve to catalog `SubjectRef`. OpenSubsonic/OPDS/Inbox ignore unknown JSON fields and keep the old wire format.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Provenance | One provider, one generation, one dimension | After ingest, `search.query` returns `lexical-metadata-fts` / `query.lexical.fts5.v1` / a non-empty generation |
| Authorization | Catalog entry must still exist | Deleted namespace rows do not appear as hits |
| Facades | Old standards consume the same operation | `TestExperienceSurfacesOverCommandABI` and `TestD5PinnedSupersonicCallSequence` stay green |

### I3 — Honest `UNAVAILABLE` for later dimensions

Status (2026-08-15): **fixture-contract-complete**.

`search.query` accepts `dimension` (top-level or inside the query object). Unknown IDs are invalid input. Known later IDs return `degraded` + `unavailable` and zero hits. They do not fall back to lexical in disguise. They do not invent a fingerprint match.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Unknown dimension | Contract error | `dimension=not-a-dimension` is invalid input |
| Acoustic / semantic / … | Declared, not shipped | `dimension=acoustic-fingerprint` is degraded; namespace/verify/restore still succeed |
| No silent fallback | Broker fusion is later | The degraded payload still names the requested dimension |

### I4 — Lexical construct axes

Status (2026-08-15): **fixture-contract-complete**.

`construct_axes` restricts the live FTS5 query to named columns. Hits report which axes actually contained a query token. This is multi-dimension *construction* of the lexical generation, not a vector index.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Axis filter | `tags:reviewed` does not hit a path-only token | Query `unique` on `construct_axes=["tags"]` misses a file whose only match is extracted text |
| Axis report | Hit lists matched axes | A tag query reports `tags` on the hit |
| Invalid axis | Contract error | `construct_axes=["lyrics"]` is invalid input |

### I5 — Isolated acoustic fingerprint processor

Status (2026-08-15): **fixture-contract-complete**, not a Chromaprint or listen-to-identify product.

An opt-in `fingerprint.audio.fixture.v1` processor emits a provider-neutral `REBUILDABLE_DERIVATIVE` artifact attached to `SubjectRef`. Default ingest does not register it. The acoustic `IndexProvider` builds a disposable generation from those artifacts. `search.query` with `dimension=acoustic-fingerprint` does exact fixture lookup (`query.acoustic.fixture.v1`, score `fixture-exact`).

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Opt-in processor | Not in `DefaultProcessors()` | Default MP3 ingest still admits only `extract.audio.tags.v1` |
| Artifact | `not_content_identity=true`; algorithm `fixture-v1` | Fingerprint string is not the file SHA-256 |
| Query | Fingerprint text or `{"fingerprint":"…"}` | Hit `SubjectRef` matches `audio.list` |
| Failure isolation | Per-node panic/fail | Tags still admit; verify/restore match SHA-256 |
| Disposable generation | Separate `index_generations.dimension` | Delete the acoustic file; acoustic search degrades; restore still matches |

Still later, not I5: Chromaprint, clip-to-fingerprint, similarity ranking, Shazam network, OpenSubsonic identify. I8 may map a *standard* client call if one exists. Do not invent `/rest/identify.view`.

### I6 — Semantic / multimodal providers

Status (2026-08-15): **fixture-contract-complete**, not embedding or CLIP products.

Opt-in `embed.text.fixture.v1` and `embed.clip.fixture.v1` emit `REBUILDABLE_DERIVATIVE` artifacts attached to `SubjectRef`. Default ingest does not register them. Each owns a disposable generation. `search.query` accepts raw text or a `sem1:` / `clip1:` token and does exact fixture lookup.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Text embed | Normalized UTF-8 sketch, prefix `sem1:` | Query the source sentence; hit the text `SubjectRef` |
| CLIP fixture | Title+artist joint string, prefix `clip1:` | Query `ClipQueryText(title, artist)`; hit the same subject as `audio.list` |
| Not identity | `not_content_identity=true` | Token is not the file SHA-256 |
| Missing generation | Degraded | Verify/restore still match |

Real model weights, vector databases, and image CLIP remain later isolated processors. The graph dimension is I9, not a second catalog.

### I7 — Host-owned fusion broker

Status (2026-08-15): **fixture-contract-complete**.

`search.query` accepts `fuse` as two or more declared dimension IDs. The broker invokes one `QueryProvider` per dimension, each against that dimension’s latest generation. Hits are unioned by `SubjectRef`. Each component keeps its own provider, generation, score semantics, and status. The broker provider is `query.broker.fuse.v1` with score semantics `component-union`. It does not invent a hybrid numeric score.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Two dimensions | `fuse=["lexical-metadata-fts","semantic-embedding"]` | Components array has both; a shared subject lists both IDs |
| No hybrid score | `score_semantics=component-union` | Result has no invented fused rank field |
| Partial loss | One generation file deleted | That component is `DEGRADED`; the other can still `SUCCEEDED` |
| Contract | `fuse` plus `dimension`, or `fuse` plus one generation | Invalid input |

### I8 — Facade mapping for identify-by-sample

Status (2026-08-15): **adapter-harness-complete**. There is no standard OpenSubsonic “identify this clip” method in the pinned client sequence.

Inbox `/inbox/api/search?dimension=acoustic-fingerprint&q=<fingerprint>` forwards to `search.query` and does **not** overlay `audio.list` / `books.list` title filters. OpenSubsonic `search2` / `search3` stay lexical. `/rest/identify.view` is rejected as an unimplemented OpenSubsonic method. Do not invent a RestoreWeave-only identify API.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Inbox passthrough | `dimension` or `fuse` query params | Acoustic fixture fingerprint returns the song `SubjectRef` |
| Old search | `search3?query=Nightfall` | Still lexical + in-memory `audio.list` |
| No private method | `identify.view` | Failed / unimplemented, not a silent lexical hit |

Qualify by replaying HTTP, the same way D5 is tested. Do not click a GUI.

### I9 — `graph-relation` catalog projection

Status (2026-08-15): **fixture-contract-complete** as a disposable projection of existing catalog facts. This is not a graph database, not new artist/album subjects, and not a second recovery authority.

`query.graph.catalog.v1` rebuilds one generation from namespace `ParentID` (`contains`), file `ContentID` (`same_content`), live TAG annotations (`tagged`), and extracted string labels (`artist` / `album` from `extract.audio.tags.v1`, `author` from `extract.book.meta.v1`). Queries are `relation:value` or `{"relation":"artist","value":"Example Artist"}`. Unknown relations are invalid input, not a lexical disguise. Score semantics are `relation-exact`.

| Piece | RestoreWeave meaning | Exit |
| --- | --- | --- |
| Artist label | String on the track subject, not a new catalog node | `artist:Example Artist` hits the same `SubjectRef` as `audio.list` |
| Tag edge | Live TAG body | `tagged:reviewed` hits the tagged file |
| Contains | Child rows whose `ParentID` is the value | `contains:<docs_id>` hits the file under that directory |
| Unknown relation | Contract error | `lyrics:foo` is invalid input |
| Disposable generation | Separate `index_generations.dimension` | Delete the graph file; graph search degrades; verify still matches SHA-256 |

Do not add `rw graph`. Use `rw search --dimension graph-relation`. OpenSubsonic `search3` stays lexical.

## 6. How this is tested

Same rule as the closed experience path:

1. Replay `capability.list` and `search.query` over the command ABI.
2. Replay OpenSubsonic `search3` and Inbox `/inbox/api/search` in-process.
3. Delete the FTS5 file; assert search degrades and verify/restore still match SHA-256.
4. Ask for `acoustic-fingerprint` without a generation; assert degraded, not a lexical disguise.
5. With the opt-in fixture processor: query the fixture fingerprint; hit the same `SubjectRef` as `audio.list`; delete the acoustic generation; verify/restore still match.
6. Opt-in text/CLIP fixtures: semantic and multimodal queries hit the same subjects; fuse lists per-component provenance without a hybrid score.
7. Inbox `dimension=acoustic-fingerprint` returns the song; `identify.view` fails; `search3` stays lexical.
8. Graph: `artist:` / `tagged:` / `contains:` hit the expected `SubjectRef`; unknown relation is invalid; delete the graph generation and verify still matches.

Do not drive Supersonic, Feishin, or KOReader to prove an index phase.

## 7. Non-goals

- Embedding a player, recognizer, or media server in `restoreweaved`.
- Making ffmpeg, Tika, Chromaprint, or a vector database core identity.
- Replacing the in-tree directory CAS.
- A fourth public REST catalog or a Unix-socket app SDK.
- Expanding requirement documents or claiming `RW-MVP-1`.
- Treating a player library, FTS file, fingerprint, or embedding as recovery authority.
