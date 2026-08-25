package controlplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	rwconfig "github.com/ailiheizi/restoreweave/config"
)

type operatorConfigState struct {
	mu            sync.Mutex
	path          string
	runningDigest string
}

type configUpdateInput struct {
	ExpectedConfigDigest string          `json:"expected_config_digest"`
	Config               json.RawMessage `json:"config"`
}

func (d *Dispatcher) handleConfigGet(env command.Envelope, started time.Time) command.Result {
	state := d.operatorConfig
	if state == nil {
		return unimplementedResult(env, started)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	data, err := state.load()
	if err != nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, err.Error()))
	}
	return succeeded(env, started, data)
}

func (d *Dispatcher) handleConfigUpdate(env command.Envelope, started time.Time) command.Result {
	state := d.operatorConfig
	if state == nil {
		return unimplementedResult(env, started)
	}
	var input configUpdateInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if strings.TrimSpace(input.ExpectedConfigDigest) == "" {
		return invalidInputResult(env, started, errors.New("expected_config_digest is required"))
	}
	if len(input.Config) == 0 || bytes.Equal(bytes.TrimSpace(input.Config), []byte("null")) {
		return invalidInputResult(env, started, errors.New("config is required"))
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	current, err := state.load()
	if err != nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, err.Error()))
	}
	if input.ExpectedConfigDigest != current.ConfigDigest {
		return conflictResult(env, started, "persisted configuration changed; reload settings before saving")
	}
	candidate, err := rwconfig.DecodeJSON(bytes.NewReader(input.Config))
	if err != nil {
		return invalidInputResult(env, started, err)
	}
	if _, err := rwconfig.Resolve(candidate, rwconfig.ResolveOptions{
		BaseDir: filepath.Dir(state.path), Environ: map[string]string{},
	}); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := rwconfig.Save(state.path, candidate); err != nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, err.Error()))
	}
	updated, err := state.load()
	if err != nil {
		return failed(env, started, newReason(ReasonCodeUnavailable, err.Error()))
	}
	return succeeded(env, started, updated)
}

func (s *operatorConfigState) load() (command.ConfigData, error) {
	resolved, err := rwconfig.LoadEffective(rwconfig.LoadOptions{
		Path: s.path,
		ResolveOptions: rwconfig.ResolveOptions{
			Environ: map[string]string{},
		},
	})
	if err != nil {
		return command.ConfigData{}, err
	}
	payload, err := json.Marshal(resolved.Config)
	if err != nil {
		return command.ConfigData{}, err
	}
	return command.ConfigData{
		ConfigPath:          resolved.ConfigPath,
		ConfigDigest:        resolved.Digest,
		RunningConfigDigest: s.runningDigest,
		RestartRequired:     resolved.Digest != s.runningDigest,
		Config:              payload,
	}, nil
}
