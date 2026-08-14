// Package protocol is a loopback OpenSubsonic / OPDS facade over the command
// ABI. It is not a player, not a second catalog, and not a public REST API.
package protocol

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/ailiheizi/restoreweave/client/command"
)

// DispatchFunc executes one command envelope. The facade never reads SQLite
// or repository packs itself.
type DispatchFunc func(ctx context.Context, env command.Envelope) command.Result

// Options pin one workspace (and optional snapshot) to one loopback listener.
type Options struct {
	WorkspaceID string
	SnapshotRef string
	Token       string
	Listen      string
}

// Server serves the compatibility facades. Bind must be loopback.
type Server struct {
	dispatch DispatchFunc
	opts     Options
}

func (opts Options) validate() error {
	if strings.TrimSpace(opts.WorkspaceID) == "" {
		return errors.New("facade workspace is required")
	}
	if strings.TrimSpace(opts.Token) == "" {
		return errors.New("facade token is required")
	}
	if strings.TrimSpace(opts.Listen) == "" {
		return errors.New("facade listen address is required")
	}
	host, _, err := net.SplitHostPort(opts.Listen)
	if err != nil {
		return fmt.Errorf("facade listen: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("facade listen must be loopback, got %q", host)
	}
	return nil
}

// New constructs a facade. It does not start a listener.
func New(dispatch DispatchFunc, opts Options) (*Server, error) {
	if dispatch == nil {
		return nil, errors.New("facade dispatcher is required")
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return &Server{dispatch: dispatch, opts: opts}, nil
}

// Handler is the HTTP surface. Callers must still bind it to loopback.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/", s.serveOpenSubsonic)
	mux.HandleFunc("/opds", s.serveOPDSNavigation)
	mux.HandleFunc("/opds/", s.serveOPDS)
	mux.HandleFunc("/inbox", s.serveInbox)
	mux.HandleFunc("/inbox/", s.serveInbox)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackRequest(r) {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		writeCORS(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func writeCORS(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
}

func (s *Server) authorized(r *http.Request) bool {
	want := s.opts.Token
	if tokenMatch(want, facadeToken(r)) {
		return true
	}
	salt := strings.TrimSpace(r.URL.Query().Get("s"))
	digest := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("t")))
	if salt == "" || digest == "" {
		return false
	}
	sum := md5.Sum([]byte(want + salt))
	got := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(digest)) == 1
}

func facadeToken(r *http.Request) string {
	token := strings.TrimSpace(r.URL.Query().Get("p"))
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if token == "" {
		if _, pass, ok := r.BasicAuth(); ok {
			token = pass
		}
	}
	if token == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}
	if len(token) > 4 && strings.EqualFold(token[:4], "enc:") {
		if raw, err := hex.DecodeString(token[4:]); err == nil {
			token = string(raw)
		}
	}
	return token
}

func tokenMatch(want, got string) bool {
	if want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) call(ctx context.Context, operation string, input any) (command.Result, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return command.Result{}, err
	}
	env, err := command.NormalizeEnvelope(command.Envelope{Operation: operation, Input: raw})
	if err != nil {
		return command.Result{}, err
	}
	result := s.dispatch(ctx, env)
	if result.Status != command.StatusSucceeded {
		return result, fmt.Errorf("%s: %s", operation, result.Status)
	}
	return result, nil
}

func loopbackRequest(r *http.Request) bool {
	if r.RemoteAddr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
