# M0: internal/identify Package

## What this milestone does

`internal/identify` is the host-owned successor of the legacy
`internal/plugin` builtin detector. The legacy `internal/plugin` prototype
(27 historical categories, manifest/entry-point machinery, external
execution design) is not the implementation target per
`docs/technical/nas-vertical-slice-implementation-plan.md`; the five active
seams are CaptureDriver, Processor, RepositoryDriver, IndexProvider, and
QueryProvider. Its most valuable code, the builtin detector, is now a
standalone package with a clean contract.

The detector rules migrate **verbatim**: the same 39 suffix rules and 24
magic rules (including the multipart RIFF patterns and the offset-257 TAR
signature), with the same canonical rule and match digests. Only the data
structures changed (slices instead of the legacy map + sorted-suffix
conflation, exported rule types). Behavior is preserved: suffix evidence and
magic evidence stay separate lines, a truncated magic match is kept with
explicit `RequiredBytesUnavailable` ranges, and a suffix/magic disagreement
yields `CONFLICTING_EVIDENCE` with both records intact — nothing is silently
overwritten. The 27-category model is not ported.

## Package structure

- `rules.go` — `FormatCandidate`, `SuffixRule`, `MagicRule`/`MagicPattern`/
  `MagicPart`, the two rule tables, `SuffixRules()`/`MagicRules()`, and
  `RulesDigest()` (sha256 pinning of the full table, for durable evidence
  provenance).
- `detect.go` — `DetectionEvidence` (kind, candidate, confidence, examined
  and missing ranges, match rule/digest, completeness), `IdentifyResult`,
  `Detector.Detect(ctx, displayName, probe)`, and the ported evidence
  classifier (`IDENTIFIED`/`AMBIGUOUS`/`CONFLICTING_EVIDENCE`/`UNKNOWN`,
  the states this detector can actually emit).
- `scanner_adapter.go` — `ScannerDetector` implementing the scanner host
  contract (see below).
- `detect_test.go` — rule-table completeness, known-magic identification,
  conflict preservation, determinism, and boundary cases (empty input,
  1 MiB probe, bounded probe truncation, case-insensitive suffixes, partial
  magic below/above the 2-byte minimum).

## Relationship to the scanner Detector interface

`internal/scanner/types.go` declares
`Detector.Detect(ctx, DetectionInput) (DetectionResult, error)` — the host
passes the full `DetectionInput` (content digest + probe + metadata), which
is a **superset** of the (digest, probe) pair the detector needs, so the
host-side signature did not match and was left untouched. `ScannerDetector`
adapts: it consumes `input.Probe` (the bounded prefix captured during
hashing) and `input.RelativePath`, maps `IdentifyResult` to
`scanner.DetectionResult` (FormatID/MediaType/Confidence on
`IDENTIFIED`, plus one `DetectionEvidence` entry per rule match), and never
receives an ambient path it can reopen.

## Wiring status

`ScannerDetector` is wired into the scanner host through the
`scanner.Config.Detector` field (`scanner.New(Config{..., Detector: sd})`),
and the end-to-end path is proven by
`internal/identify/scanner_adapter_test.go` (3 tests, all green):

- `TestScannerDetectorKnownFormatsFlowIntoEntryDetection` — a real tree
  (`a.txt`, `photo.jpg` with a JPEG magic prefix, `nested/pic.png`) scanned
  with `scanner.OSFileSystem`, an in-memory `scanner.Sink`, and the adapter;
  asserts `EntryRecord.Detection.State == SUCCEEDED`, `FormatID`/`MediaType`/
  `Confidence` on magic-confirmed files, the exact
  `Evidence[].Method`/`Value` lines (`suffix:.jpg`, `magic:jpeg-soi`), and
  that directory records stay `NOT_REQUESTED`.
- `TestScannerDetectorUnknownFormatHasNoFalsePositive` — random `data.bin`
  with no suffix or magic match claims no format and records no evidence.
- `TestScannerDetectorEmptyDirectoryScanCompletes` — empty tree scans to
  `ScanComplete` with only directory entries.

Observed contract behavior (asserted as-is, not papered over): a suffix-only
file like `a.txt` is `AMBIGUOUS` inside identify, so `mapToScannerResult`
leaves `FormatID`/`MediaType`/`Confidence` empty while still recording the
`suffix:.txt` evidence line; only magic-confirmed formats (`.jpg`, `.png`)
receive filled format fields with `Confidence = 1`.

Verification commands:

```sh
go build ./...
go test -count=1 ./...
go vet ./...
```

## Remaining TODO

- Record `RulesDigest()` alongside durable evidence so a rule change is
  visible as a new evidence generation, per the reclassification section of
  `docs/requirements/file-identification-and-extraction.md`.

`internal/plugin` currently has **no callers** (verified with
`go list ./...` and an import search; no package outside `internal/plugin`
imports it) and is marked deprecated in `manifest.go`. It may be deleted in
a later milestone once this package is wired and its tests are green, and
the builtin detector in particular is fully superseded by `internal/identify`.
