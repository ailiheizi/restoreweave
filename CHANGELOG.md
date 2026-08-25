# Changelog

## Unreleased

- Licensed the RestoreWeave project source under the MIT License.
- Added detailed Chinese and English public README guides.
- Reconciled public capability and roadmap wording with the tested core preview.
- Marked the removed Inbox, OpenSubsonic, and OPDS adapters as historical.

## v0.1.0-prealpha.1 - 2026-08-25

First core-preview checkpoint. This is a source build for development and
evaluation, not a production-qualified release.

### Core workflow

- Added configured directory inspection, reviewed protection plans, exact
  whole-file SHA-256 identity, duplicate accounting, verification, and restore.
- Added durable versioned Notes, lexical/structured search, saved views, and
  frozen export manifests for the admitted local scope.
- Added real local `BAAI/bge-small-zh-v1.5` ONNX inference and in-process zvec
  generations, including startup probing, restart reopening, leases, and honest
  unavailable behavior.
- Added the optional loopback `/api/v1` adapter and React browser client over
  the existing typed command dispatcher.

### Recovery and safety

- Added authenticated portable facts and subject mappings, independent trust
  anchors, clean-install reading, corruption rejection, relocation evidence,
  cross-process publication fencing, and unknown-outcome reconciliation.
- Added bounded processor retry of the same signed plan and copy-forward
  repository migration evidence.
- Kept garbage collection non-destructive; source deletion and automatic
  external reacquisition remain disabled.

### Scope cleanup

- Removed the experimental Inbox, OpenSubsonic, OPDS, and gateway implementation.
- Kept CLI as an initialization, diagnostics, scripting, and emergency-recovery
  surface; the browser client remains a thin adapter over the same core.

### Known limitations

- No supported installer, container image, or bundled model download.
- Repository profiles and release migration are not production-qualified.
- No destructive GC, writable filesystem gateway, or automatic source cleanup.
