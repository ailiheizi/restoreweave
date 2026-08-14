package command

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaCommand    = "org.restoreweave.command.v1"
	SchemaResult     = "org.restoreweave.result.v1"
	DefaultWorkspace = "workspace:default"
)

type Status string

const (
	StatusAccepted               Status = "ACCEPTED"
	StatusSucceeded              Status = "SUCCEEDED"
	StatusDegraded               Status = "DEGRADED"
	StatusBlocked                Status = "BLOCKED"
	StatusFailed                 Status = "FAILED"
	StatusCancelled              Status = "CANCELLED"
	StatusUnknownExternalOutcome Status = "UNKNOWN_EXTERNAL_OUTCOME"
)

type Envelope struct {
	Schema         string          `json:"schema"`
	RequestID      string          `json:"request_id"`
	Operation      string          `json:"operation"`
	WorkspaceRef   string          `json:"workspace_ref"`
	IdempotencyKey *string         `json:"idempotency_key"`
	Input          json.RawMessage `json:"input"`
}

type Result struct {
	Schema       string            `json:"schema"`
	RequestID    string            `json:"request_id"`
	Operation    string            `json:"operation"`
	Status       Status            `json:"status"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at"`
	JobRef       *string           `json:"job_ref"`
	ResourceRefs map[string]string `json:"resource_refs"`
	Reasons      []Reason          `json:"reasons"`
	Artifacts    []json.RawMessage `json:"artifacts"`
	Data         json.RawMessage   `json:"data"`
}

type Reason struct {
	Code       string         `json:"code"`
	Class      string         `json:"class"`
	Severity   string         `json:"severity"`
	Message    string         `json:"message"`
	Retryable  bool           `json:"retryable"`
	SubjectRef string         `json:"subject_ref,omitempty"`
	Resolution *Resolution    `json:"resolution,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

type Resolution struct {
	Action    string         `json:"action"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func NewRequestID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}

func NormalizeEnvelope(env Envelope) (Envelope, error) {
	if env.Schema == "" {
		env.Schema = SchemaCommand
	}
	if env.Schema != SchemaCommand {
		return Envelope{}, fmt.Errorf("unsupported command schema %q", env.Schema)
	}
	env.Operation = strings.TrimSpace(env.Operation)
	if env.Operation == "" {
		return Envelope{}, fmt.Errorf("operation is required")
	}
	if env.RequestID == "" {
		env.RequestID = NewRequestID()
	}
	if strings.TrimSpace(env.WorkspaceRef) == "" {
		env.WorkspaceRef = DefaultWorkspace
	}
	if len(env.Input) == 0 {
		env.Input = json.RawMessage(`{}`)
	}
	if !json.Valid(env.Input) {
		return Envelope{}, fmt.Errorf("input is not valid JSON")
	}
	return env, nil
}

func NewResult(env Envelope, status Status, started, finished time.Time, data any, reasons ...Reason) Result {
	payload := json.RawMessage(`{}`)
	if data != nil {
		encoded, err := json.Marshal(data)
		if err == nil {
			payload = encoded
		}
	}
	if reasons == nil {
		reasons = []Reason{}
	}
	refs := env.ResourceRefs()
	return Result{
		Schema:       SchemaResult,
		RequestID:    env.RequestID,
		Operation:    env.Operation,
		Status:       status,
		StartedAt:    started.UTC().Truncate(time.Second),
		FinishedAt:   finished.UTC().Truncate(time.Second),
		ResourceRefs: refs,
		Reasons:      reasons,
		Artifacts:    []json.RawMessage{},
		Data:         payload,
	}
}

func (env Envelope) ResourceRefs() map[string]string {
	return map[string]string{
		"workspace_ref": env.WorkspaceRef,
	}
}

func (r Result) ExitCode() int {
	switch r.Status {
	case StatusSucceeded, StatusAccepted:
		return 0
	case StatusBlocked:
		return 3
	case StatusFailed:
		return 4
	case StatusDegraded:
		return 5
	case StatusCancelled:
		return 6
	case StatusUnknownExternalOutcome:
		return 7
	default:
		return 4
	}
}
