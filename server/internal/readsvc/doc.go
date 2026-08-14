// Package readsvc defines RestoreWeave's storage-agnostic, read-only snapshot
// namespace and content-access seams.
//
// The package intentionally separates host policy from adapter mechanics.
// Snapshot authorization, component-by-component path resolution, placement
// selection, and verification acceptance remain host responsibilities. Storage,
// repository, decoder, and filesystem-gateway adapters receive only bounded,
// host-selected requests and may emit evidence claims, never policy decisions.
package readsvc
