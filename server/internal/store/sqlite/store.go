// Package sqlite implements RestoreWeave's rebuildable operational catalog.
// Signed manifests, placement ledgers, receipts, and audit exports remain
// independently durable recovery authority; this package only indexes them.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Options struct {
	BusyTimeout  time.Duration
	MaxOpenConns int
	Now          func() time.Time
}

type Store struct {
	db  *sql.DB
	now func() time.Time
}

type Tx struct {
	tx  *sql.Tx
	now func() time.Time
}

type RuntimePragmas struct {
	JournalMode string
	ForeignKeys bool
	BusyTimeout time.Duration
	Synchronous int
}

func Open(ctx context.Context, path string, options Options) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite store path is required")
	}
	if options.BusyTimeout <= 0 {
		options.BusyTimeout = 5 * time.Second
	}
	if options.MaxOpenConns <= 0 {
		options.MaxOpenConns = 4
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	dsn, memory, err := sqliteDSN(path, options.BusyTimeout)
	if err != nil {
		return nil, err
	}
	if !memory && !strings.HasPrefix(path, "file:") {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve sqlite path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			return nil, fmt.Errorf("create sqlite parent directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite catalog: %w", err)
	}
	db.SetMaxOpenConns(options.MaxOpenConns)
	db.SetMaxIdleConns(options.MaxOpenConns)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db, now: options.Now}
	closeOnError := func(openErr error) (*Store, error) {
		_ = db.Close()
		return nil, openErr
	}
	if err := db.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("ping sqlite catalog: %w", err))
	}
	if !memory {
		var mode string
		if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL`).Scan(&mode); err != nil {
			return closeOnError(fmt.Errorf("enable sqlite WAL: %w", err))
		}
		if !strings.EqualFold(mode, "wal") {
			return closeOnError(fmt.Errorf("enable sqlite WAL: journal mode is %q", mode))
		}
	}
	if err := store.migrate(ctx); err != nil {
		return closeOnError(err)
	}
	pragmas, err := store.RuntimePragmas(ctx)
	if err != nil {
		return closeOnError(err)
	}
	if !pragmas.ForeignKeys {
		return closeOnError(errors.New("sqlite foreign key enforcement is disabled"))
	}
	return store, nil
}

func sqliteDSN(path string, busyTimeout time.Duration) (string, bool, error) {
	memory := path == ":memory:"
	var parsed *url.URL
	var err error
	if memory {
		id, idErr := NewStableID("mem")
		if idErr != nil {
			return "", false, idErr
		}
		parsed, err = url.Parse("file:" + id + "?mode=memory&cache=shared")
	} else if strings.HasPrefix(path, "file:") {
		parsed, err = url.Parse(path)
		if err == nil && parsed.Query().Get("mode") == "memory" {
			memory = true
		}
	} else {
		absolute, absErr := filepath.Abs(path)
		if absErr != nil {
			return "", false, fmt.Errorf("resolve sqlite path: %w", absErr)
		}
		parsed = &url.URL{Scheme: "file", Path: absolute}
	}
	if err != nil {
		return "", false, fmt.Errorf("parse sqlite path: %w", err)
	}

	query := parsed.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	// FULL is intentional. Authority-affecting transitions may share this
	// catalog even though it remains a rebuildable projection. A future relaxed
	// ingest connection must be separate and must not commit those transitions.
	query.Add("_pragma", "synchronous(FULL)")
	query.Set("_txlock", "immediate")
	parsed.RawQuery = query.Encode()
	return parsed.String(), memory, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) RuntimePragmas(ctx context.Context) (RuntimePragmas, error) {
	var state RuntimePragmas
	var enabled int
	var busyMS int64
	if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&state.JournalMode); err != nil {
		return state, fmt.Errorf("read sqlite journal mode: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		return state, fmt.Errorf("read sqlite foreign key mode: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyMS); err != nil {
		return state, fmt.Errorf("read sqlite busy timeout: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&state.Synchronous); err != nil {
		return state, fmt.Errorf("read sqlite synchronous mode: %w", err)
	}
	state.ForeignKeys = enabled == 1
	state.BusyTimeout = time.Duration(busyMS) * time.Millisecond
	return state, nil
}

// Update executes fn atomically. The DSN requests BEGIN IMMEDIATE so two
// writers cannot both observe an unclaimed idempotency key or lease.
func (s *Store) Update(ctx context.Context, fn func(*Tx) error) error {
	if fn == nil {
		return errors.New("sqlite update callback is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite transaction: %w", err)
	}
	defer tx.Rollback()
	if err := fn(&Tx{tx: tx, now: s.now}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}
	return nil
}

// Idempotent runs fn and records its response in the same write transaction.
// A retry with the same scope, key, and request hash replays the stored result
// without invoking fn. Reusing the key for different input fails closed.
func (s *Store) Idempotent(
	ctx context.Context,
	request IdempotencyRequest,
	fn func(*Tx) (IdempotencyResult, error),
) (result IdempotencyResult, replayed bool, err error) {
	if err := validateStableID(request.WorkspaceID); err != nil {
		return result, false, fmt.Errorf("workspace id: %w", err)
	}
	if strings.TrimSpace(request.Scope) == "" || strings.TrimSpace(request.Key) == "" || strings.TrimSpace(request.RequestHash) == "" {
		return result, false, errors.New("idempotency workspace, scope, key, and request hash are required")
	}
	if fn == nil {
		return result, false, errors.New("idempotency callback is required")
	}

	err = s.Update(ctx, func(tx *Tx) error {
		var storedRequestHash string
		var response string
		rowErr := tx.tx.QueryRowContext(ctx, `
SELECT request_hash, resource_type, resource_id, response_json
FROM idempotency_records
WHERE workspace_id = ? AND scope = ? AND idempotency_key = ?`,
			request.WorkspaceID, request.Scope, request.Key).Scan(
			&storedRequestHash, &result.ResourceType, &result.ResourceID, &response)
		if rowErr == nil {
			if storedRequestHash != request.RequestHash {
				return ErrIdempotencyConflict
			}
			result.Response = json.RawMessage(response)
			replayed = true
			return nil
		}
		if !errors.Is(rowErr, sql.ErrNoRows) {
			return fmt.Errorf("read idempotency record: %w", rowErr)
		}

		var callbackErr error
		result, callbackErr = fn(tx)
		if callbackErr != nil {
			return callbackErr
		}
		responseJSON, normalizeErr := normalizeJSON(result.Response)
		if normalizeErr != nil {
			return fmt.Errorf("idempotency response: %w", normalizeErr)
		}
		result.Response = responseJSON
		_, insertErr := tx.tx.ExecContext(ctx, `
INSERT INTO idempotency_records(
    workspace_id, scope, idempotency_key, request_hash,
    resource_type, resource_id, response_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			request.WorkspaceID, request.Scope, request.Key, request.RequestHash,
			result.ResourceType, result.ResourceID, string(responseJSON), tx.now().UTC().UnixNano())
		if insertErr != nil {
			return fmt.Errorf("record idempotent result: %w", insertErr)
		}
		return nil
	})
	return result, replayed, err
}

func normalizeJSON(value json.RawMessage) (json.RawMessage, error) {
	if len(value) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(value) {
		return nil, errors.New("value is not valid JSON")
	}
	return append(json.RawMessage(nil), value...), nil
}

func recordTime(value time.Time, now func() time.Time) time.Time {
	if value.IsZero() {
		value = now()
	}
	return value.UTC()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableBytes(value []byte) any {
	if value == nil {
		return nil
	}
	return value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixNano()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func requireID(name, value string) error {
	if err := validateStableID(value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func requireText(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func insertOne(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
