package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	rwconfig "github.com/ailiheizi/restoreweave/config"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestDispatcherConfigGetAndUpdatePersistValidatedProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "restoreweave.toml")
	resolved, err := rwconfig.Init(configPath)
	if err != nil {
		t.Fatal(err)
	}
	store := testutil.OpenStore(t, ":memory:")
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithOperatorConfig(resolved))

	got := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpConfigGet, map[string]any{}))
	if got.Status != command.StatusSucceeded {
		t.Fatalf("config.get = %q: %+v", got.Status, got.Reasons)
	}
	var data command.ConfigData
	if err := json.Unmarshal(got.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.ConfigPath != configPath || data.ConfigDigest != resolved.Digest || data.RunningConfigDigest != resolved.Digest || data.RestartRequired {
		t.Fatalf("config.get data = %+v", data)
	}
	var candidate rwconfig.Config
	if err := json.Unmarshal(data.Config, &candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Semantic.EmbeddingMode = "hybrid"
	candidate.Semantic.OnlineProfile = "operator-semantic-v1"
	candidate.Semantic.OnlineCredentialRef = "keychain://restoreweave/semantic"
	payload, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	updated := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpConfigUpdate, map[string]any{
		"expected_config_digest": data.ConfigDigest,
		"config":                 json.RawMessage(payload),
	}))
	if updated.Status != command.StatusSucceeded {
		t.Fatalf("config.update = %q: %+v", updated.Status, updated.Reasons)
	}
	var updatedData command.ConfigData
	if err := json.Unmarshal(updated.Data, &updatedData); err != nil {
		t.Fatal(err)
	}
	if updatedData.ConfigDigest == data.ConfigDigest || !updatedData.RestartRequired || updatedData.RunningConfigDigest != resolved.Digest {
		t.Fatalf("updated config data = %+v", updatedData)
	}
	persisted, err := rwconfig.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Semantic.EmbeddingMode != "hybrid" || persisted.Semantic.OnlineProfile != "operator-semantic-v1" || persisted.Semantic.OnlineCredentialRef != "keychain://restoreweave/semantic" {
		t.Fatalf("persisted semantic config = %+v", persisted.Semantic)
	}
	info, err := os.Stat(configPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

func TestDispatcherConfigUpdateRejectsStaleAndInvalidProfiles(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "restoreweave.toml")
	resolved, err := rwconfig.Init(configPath)
	if err != nil {
		t.Fatal(err)
	}
	store := testutil.OpenStore(t, ":memory:")
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithOperatorConfig(resolved))
	payload, err := json.Marshal(resolved.Config)
	if err != nil {
		t.Fatal(err)
	}
	stale := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpConfigUpdate, map[string]any{
		"expected_config_digest": "sha256:stale",
		"config":                 json.RawMessage(payload),
	}))
	if stale.Status != command.StatusFailed || !hasReasonCode(stale, ReasonCodeConflict) {
		t.Fatalf("stale config.update = %q: %+v", stale.Status, stale.Reasons)
	}

	var candidate rwconfig.Config
	if err := json.Unmarshal(payload, &candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Semantic.EmbeddingMode = "online"
	candidate.Semantic.OnlineProfile = "provider-v1"
	candidate.Semantic.OnlineCredentialRef = "api_key=plaintext-secret"
	invalidPayload, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	invalid := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpConfigUpdate, map[string]any{
		"expected_config_digest": resolved.Digest,
		"config":                 json.RawMessage(invalidPayload),
	}))
	if invalid.Status != command.StatusFailed || !hasReasonCode(invalid, ReasonCodeInvalidInput) {
		t.Fatalf("invalid config.update = %q: %+v", invalid.Status, invalid.Reasons)
	}
	unchanged, err := rwconfig.LoadEffective(rwconfig.LoadOptions{Path: configPath, ResolveOptions: rwconfig.ResolveOptions{Environ: map[string]string{}}})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Digest != resolved.Digest {
		t.Fatalf("invalid update changed config digest to %s", unchanged.Digest)
	}
}
