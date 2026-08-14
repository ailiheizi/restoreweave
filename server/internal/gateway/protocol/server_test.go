package protocol

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
)

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
