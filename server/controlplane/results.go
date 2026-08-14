package controlplane

import (
	"encoding/json"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

// Reason codes returned inside command.Result.Reasons.
const (
	ReasonCodeUnknownOperation    = "unknown_operation"
	ReasonCodeUnimplemented       = "unimplemented"
	ReasonCodeInvalidRequest      = "invalid_request"
	ReasonCodeInvalidInput        = "invalid_input"
	ReasonCodeNotFound            = "not_found"
	ReasonCodeConflict            = "conflict"
	ReasonCodeCatalogError        = "catalog_error"
	ReasonCodeUnavailable         = "unavailable"
	ReasonCodeUnsupportedPlatform = "unsupported_platform"
)

// IdentifyBuiltinID names the host-owned suffix and magic-byte detector
// served by this daemon build.
const IdentifyBuiltinID = "identify:builtin"

func newReason(code, message string) command.Reason {
	return command.Reason{
		Code:      code,
		Class:     "operation",
		Severity:  "error",
		Message:   message,
		Retryable: false,
	}
}

// failedResult builds a FAILED result for an envelope that could not be
// normalized (and therefore carries no guaranteed request id).
func failedRawResult(raw command.Envelope, started time.Time, reason command.Reason) command.Result {
	return command.Result{
		Schema:       command.SchemaResult,
		RequestID:    raw.RequestID,
		Operation:    raw.Operation,
		Status:       command.StatusFailed,
		StartedAt:    started.UTC().Truncate(time.Second),
		FinishedAt:   time.Now().UTC().Truncate(time.Second),
		ResourceRefs: map[string]string{"workspace_ref": raw.WorkspaceRef},
		Reasons:      []command.Reason{reason},
		Artifacts:    []json.RawMessage{},
		Data:         json.RawMessage(`{}`),
	}
}

func succeeded(env command.Envelope, started time.Time, data any) command.Result {
	return command.NewResult(env, command.StatusSucceeded, started, time.Now().UTC(), data)
}

func failed(env command.Envelope, started time.Time, reason command.Reason) command.Result {
	return command.NewResult(env, command.StatusFailed, started, time.Now().UTC(), nil, reason)
}

func unimplementedResult(env command.Envelope, started time.Time) command.Result {
	return failed(env, started, newReason(
		ReasonCodeUnimplemented,
		"operation "+env.Operation+" is not implemented by this restoreweaved build",
	))
}

func unknownOperationResult(env command.Envelope, started time.Time) command.Result {
	return failed(env, started, newReason(
		ReasonCodeUnknownOperation,
		"unknown operation "+env.Operation,
	))
}

func notFoundResult(env command.Envelope, started time.Time, message string) command.Result {
	return failed(env, started, newReason(ReasonCodeNotFound, message))
}

func catalogErrorResult(env command.Envelope, started time.Time, err error) command.Result {
	return failed(env, started, newReason(ReasonCodeCatalogError, err.Error()))
}

func conflictResult(env command.Envelope, started time.Time, message string) command.Result {
	return failed(env, started, newReason(ReasonCodeConflict, message))
}

func degradedResult(env command.Envelope, started time.Time, data any, message string) command.Result {
	return command.NewResult(env, command.StatusDegraded, started, time.Now().UTC(), data, newReason(
		ReasonCodeUnavailable, message,
	))
}

// validStableID mirrors the catalog's stable-ID shape (prefix_32-hex) so the
// control plane can reject malformed workspace/entry references with a clean
// invalid_input reason instead of a catalog round trip. It is deliberately an
// independent implementation; the control plane must not rely on the store's
// private helpers.
func validStableID(value string) bool {
	prefix, payload, ok := splitPrefix(value)
	if !ok {
		return false
	}
	if len(prefix) < 2 || len(prefix) > 12 {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		char := prefix[i]
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	if len(payload) != 32 {
		return false
	}
	for i := 0; i < len(payload); i++ {
		char := payload[i]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func splitPrefix(value string) (string, string, bool) {
	for i := 0; i < len(value); i++ {
		if value[i] == '_' {
			return value[:i], value[i+1:], true
		}
	}
	return "", "", false
}

// hasReasonCode reports whether the result carries the given reason code.
func hasReasonCode(result command.Result, code string) bool {
	for _, reason := range result.Reasons {
		if reason.Code == code {
			return true
		}
	}
	return false
}
