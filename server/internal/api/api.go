// Package api exposes a deliberately thin HTTP adapter for the typed command
// dispatcher. It owns transport concerns only; policy, storage, jobs, and
// recovery remain in controlplane.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
)

type DispatchFunc func(context.Context, command.Envelope) command.Result

type Options struct {
	// Token is optional for loopback development. When set, requests must send
	// Authorization: Bearer <token>. The token is injected by the host and is
	// never read from a request body or persisted catalog.
	Token string
}

func Handler(dispatch DispatchFunc, options Options) http.Handler {
	if dispatch == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusInternalServerError, "dispatcher is required")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/healthz" {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if strings.TrimSpace(options.Token) != "" && r.Header.Get("Authorization") != "Bearer "+options.Token {
			writeError(w, http.StatusUnauthorized, "authorization required")
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/command" {
			writeError(w, http.StatusNotFound, "endpoint not found")
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "read request: "+err.Error())
			return
		}
		if len(body) == 0 || !json.Valid(body) {
			writeError(w, http.StatusBadRequest, "request body must be valid JSON")
			return
		}
		var envelope command.Envelope
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&envelope); err != nil {
			writeError(w, http.StatusBadRequest, "decode command: "+err.Error())
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "request must contain one JSON object")
			return
		}
		result := dispatch(r.Context(), envelope)
		status := http.StatusOK
		if result.Status == command.StatusFailed || result.Status == command.StatusBlocked {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, result)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
