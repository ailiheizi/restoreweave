package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/api"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestHTTPAPIUsesTheSameDispatcherResult(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/restoreweave-test.sock")
	handler := api.Handler(dispatcher.Handle, api.Options{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/command", strings.NewReader(`{"operation":"status.get","input":{}}`))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("http status = %d body=%s", resp.Code, resp.Body.String())
	}
	var remote command.Result
	if err := json.Unmarshal(resp.Body.Bytes(), &remote); err != nil {
		t.Fatal(err)
	}
	direct := dispatcher.Handle(context.Background(), command.Envelope{Operation: command.OpStatusGet, Input: []byte(`{}`)})
	if remote.Status != direct.Status || string(remote.Data) != string(direct.Data) {
		t.Fatalf("remote=%s direct=%s", resp.Body.String(), mustJSON(t, direct))
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
