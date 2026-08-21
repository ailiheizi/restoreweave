package protocol

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

func TestInboxRestorePlansOnlyWithoutDestination(t *testing.T) {
	var operations []string
	server := newInboxRestoreTestServer(t, func(_ context.Context, env command.Envelope) command.Result {
		operations = append(operations, env.Operation)
		if env.Operation != command.OpPlanRestore {
			t.Fatalf("unexpected operation %q", env.Operation)
		}
		return command.NewResult(env, command.StatusSucceeded, time.Now(), time.Now(), command.PlanRestoreData{
			SnapshotRef: "snapshot-1", Files: 2, Bytes: 12, PlanID: "plan-1", PlanDigest: "digest-1",
			State: "READY",
		})
	})
	defer server.Close()

	resp := postInboxRestore(t, server.URL, "{}")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	resp.Body.Close()
	if payload["wrote"] != false || payload["plan_id"] != "plan-1" {
		t.Fatalf("preflight response = %#v", payload)
	}
	if len(operations) != 1 || operations[0] != command.OpPlanRestore {
		t.Fatalf("operations = %#v, want plan.restore only", operations)
	}
}

func TestInboxRestoreAppliesPlanWithDestination(t *testing.T) {
	var operations []string
	var applyInput map[string]any
	server := newInboxRestoreTestServer(t, func(_ context.Context, env command.Envelope) command.Result {
		operations = append(operations, env.Operation)
		switch env.Operation {
		case command.OpPlanRestore:
			var input map[string]any
			if err := json.Unmarshal(env.Input, &input); err != nil {
				t.Fatalf("decode plan.restore input: %v", err)
			}
			return command.NewResult(env, command.StatusSucceeded, time.Now(), time.Now(), command.PlanRestoreData{
				WorkspaceID: "wsp_plan", SnapshotRef: "snapshot-1", Destination: input["destination"].(string), Files: 2, Bytes: 12,
				PlanID: "plan-1", PlanDigest: "digest-1", State: "READY", Executable: true,
			})
		case command.OpPlanApply:
			if err := json.Unmarshal(env.Input, &applyInput); err != nil {
				t.Fatalf("decode plan.apply input: %v", err)
			}
			return command.NewResult(env, command.StatusSucceeded, time.Now(), time.Now(), command.PlanApplyData{
				PlanID: "plan-1", PlanDigest: "digest-1", SnapshotRef: "snapshot-1", Destination: "/tmp/out",
				Files: 2, Bytes: 12,
			})
		default:
			t.Fatalf("unexpected operation %q", env.Operation)
			return command.Result{}
		}
	})
	defer server.Close()

	resp := postInboxRestore(t, server.URL, `{"destination":"/tmp/out"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	resp.Body.Close()
	if payload["wrote"] != true || payload["destination"] != "/tmp/out" {
		t.Fatalf("restore response = %#v", payload)
	}
	if len(operations) != 2 || operations[0] != command.OpPlanRestore || operations[1] != command.OpPlanApply {
		t.Fatalf("operations = %#v, want plan.restore then plan.apply", operations)
	}
	if applyInput["workspace_id"] != "wsp_plan" || applyInput["plan_id"] != "plan-1" || applyInput["plan_digest"] != "digest-1" {
		t.Fatalf("plan.apply input = %#v", applyInput)
	}
}

func newInboxRestoreTestServer(t *testing.T, dispatch DispatchFunc) *httptest.Server {
	t.Helper()
	server, err := New(dispatch, Options{
		WorkspaceID: "ws_test", Token: "inbox-token", SnapshotRef: "snapshot-1", Listen: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return httptest.NewServer(server.Handler())
}

func postInboxRestore(t *testing.T, baseURL, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/inbox/api/restore?p=inbox-token", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("new restore request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("restore request: %v", err)
	}
	return resp
}

func TestNewRejectsNonLoopbackListen(t *testing.T) {
	_, err := New(func(ctx context.Context, env command.Envelope) command.Result {
		return command.Result{}
	}, Options{
		WorkspaceID: "ws_test",
		Token:       "secret",
		Listen:      "0.0.0.0:4534",
	})
	if err == nil {
		t.Fatal("expected non-loopback listen to fail")
	}
}

func TestNewRejectsEmptyToken(t *testing.T) {
	_, err := New(func(ctx context.Context, env command.Envelope) command.Result {
		return command.Result{}
	}, Options{
		WorkspaceID: "ws_test",
		Listen:      "127.0.0.1:4534",
	})
	if err == nil {
		t.Fatal("expected empty token to fail")
	}
}

func TestFacadeAdaptsExistingOpenSubsonicClients(t *testing.T) {
	facade, err := New(func(ctx context.Context, env command.Envelope) command.Result {
		return command.Result{Status: command.StatusSucceeded, Data: json.RawMessage(`{"tracks":[],"albums":[]}`)}
	}, Options{
		WorkspaceID: "ws_test",
		Token:       "facade-token",
		Listen:      "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("new facade: %v", err)
	}
	server := httptest.NewServer(facade.Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodOptions, server.URL+"/rest/ping.view", nil)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	req.Header.Set("Origin", "http://localhost:4333")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do options: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:4333" {
		t.Fatalf("CORS preflight status=%d origin=%s", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}

	enc := "enc:" + hex.EncodeToString([]byte("facade-token"))
	ping, err := http.Get(server.URL + "/rest/ping.view?f=json&p=" + enc)
	if err != nil {
		t.Fatalf("enc ping: %v", err)
	}
	var encPayload map[string]any
	if err := json.NewDecoder(ping.Body).Decode(&encPayload); err != nil {
		t.Fatalf("decode enc ping: %v", err)
	}
	ping.Body.Close()
	inner, _ := encPayload["subsonic-response"].(map[string]any)
	if inner["status"] != "ok" {
		t.Fatalf("enc ping = %#v", encPayload)
	}

	salt := "abc"
	sum := md5.Sum([]byte("facade-token" + salt))
	tokenPing, err := http.Get(server.URL + "/rest/ping.view?f=json&u=local&s=" + salt + "&t=" + hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("token ping: %v", err)
	}
	var tokenPayload map[string]any
	if err := json.NewDecoder(tokenPing.Body).Decode(&tokenPayload); err != nil {
		t.Fatalf("decode token ping: %v", err)
	}
	tokenPing.Body.Close()
	inner, _ = tokenPayload["subsonic-response"].(map[string]any)
	if inner["status"] != "ok" {
		t.Fatalf("token ping = %#v", tokenPayload)
	}

	genres, err := http.Get(server.URL + "/rest/getGenres.view?f=json&p=facade-token")
	if err != nil {
		t.Fatalf("genres: %v", err)
	}
	var genrePayload map[string]any
	if err := json.NewDecoder(genres.Body).Decode(&genrePayload); err != nil {
		t.Fatalf("decode genres: %v", err)
	}
	genres.Body.Close()
	inner, _ = genrePayload["subsonic-response"].(map[string]any)
	if inner["status"] != "ok" {
		t.Fatalf("getGenres = %#v", genrePayload)
	}
}
