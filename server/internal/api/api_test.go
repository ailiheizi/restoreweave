package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

var commandTime = time.Unix(0, 0)

func TestCommandAdapterDelegatesExactEnvelope(t *testing.T) {
	var got command.Envelope
	h := Handler(func(_ context.Context, env command.Envelope) command.Result {
		got = env
		return command.NewResult(env, command.StatusSucceeded, commandTime, commandTime, map[string]string{"ok": "yes"})
	}, Options{Token: "secret"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/command", strings.NewReader(`{"operation":"status.get","input":{}}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || got.Operation != command.OpStatusGet {
		t.Fatalf("status=%d envelope=%+v body=%s", resp.Code, got, resp.Body.String())
	}
	var result command.Result
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil || result.Status != command.StatusSucceeded {
		t.Fatalf("result=%s err=%v", resp.Body, err)
	}
}

func TestCommandAdapterRejectsMissingTokenAndUnknownRoute(t *testing.T) {
	h := Handler(func(_ context.Context, env command.Envelope) command.Result {
		return command.NewResult(env, command.StatusSucceeded, commandTime, commandTime, nil)
	}, Options{Token: "secret"})
	for _, test := range []struct {
		name string
		path string
		want int
	}{
		{"token", "/api/v1/command", http.StatusUnauthorized},
		{"route", "/api/v1/status", http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"operation":"status.get"}`))
			resp := httptest.NewRecorder()
			h.ServeHTTP(resp, req)
			if resp.Code != test.want {
				t.Fatalf("status=%d want=%d", resp.Code, test.want)
			}
		})
	}
}
