package command

import "testing"

func TestNormalizeEnvelopeDefaults(t *testing.T) {
	env, err := NormalizeEnvelope(Envelope{Operation: OpStatusGet})
	if err != nil {
		t.Fatalf("NormalizeEnvelope: %v", err)
	}
	if env.Schema != SchemaCommand {
		t.Fatalf("schema = %q", env.Schema)
	}
	if env.WorkspaceRef != DefaultWorkspace {
		t.Fatalf("workspace = %q", env.WorkspaceRef)
	}
	if env.RequestID == "" {
		t.Fatal("request id was not generated")
	}
	if string(env.Input) != "{}" {
		t.Fatalf("input = %s", env.Input)
	}
}

func TestNormalizeEnvelopeRejectsUnknownSchema(t *testing.T) {
	_, err := NormalizeEnvelope(Envelope{Schema: "other", Operation: OpStatusGet})
	if err == nil {
		t.Fatal("expected schema error")
	}
}

func TestResultExitCode(t *testing.T) {
	if (Result{Status: StatusSucceeded}).ExitCode() != 0 {
		t.Fatal("succeeded should exit 0")
	}
	if (Result{Status: StatusBlocked}).ExitCode() != 3 {
		t.Fatal("blocked should exit 3")
	}
}
