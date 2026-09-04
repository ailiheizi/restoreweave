package controlplane

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestDoctorAndStatusReportLocalZstdProfile(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenZstdDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	doctorResult := dispatcher.Handle(ctx, mustEnvelope(t, command.OpDoctorCheck, map[string]any{}))
	if doctorResult.Status != command.StatusSucceeded {
		t.Fatalf("doctor = %q: %+v", doctorResult.Status, doctorResult.Reasons)
	}
	var doctor command.DoctorData
	if err := json.Unmarshal(doctorResult.Data, &doctor); err != nil {
		t.Fatal(err)
	}
	repositoryReported := false
	engineHonest := false
	for _, check := range doctor.Checks {
		if check.ID == "repository" && strings.Contains(check.Message, repository.RepositoryProfileLocalZstdV1) &&
			strings.Contains(check.Message, repository.CompressionProfileZstdV1) {
			repositoryReported = true
		}
		if check.ID == "engine" && strings.Contains(check.Message, repository.RepositoryProfileLocalZstdV1) &&
			strings.Contains(check.Message, "not a selected release engine") {
			engineHonest = true
		}
	}
	if !repositoryReported || !engineHonest {
		t.Fatalf("doctor profile checks = %+v", doctor.Checks)
	}

	statusResult := dispatcher.Handle(ctx, mustEnvelope(t, command.OpStatusGet, map[string]any{}))
	if statusResult.Status != command.StatusSucceeded {
		t.Fatalf("status = %q: %+v", statusResult.Status, statusResult.Reasons)
	}
	var status command.StatusData
	if err := json.Unmarshal(statusResult.Data, &status); err != nil {
		t.Fatal(err)
	}
	if status.Repository == nil || status.Repository.RepositoryProfile != repository.RepositoryProfileLocalZstdV1 ||
		status.Repository.CompressionProfile != repository.CompressionProfileZstdV1 {
		t.Fatalf("repository status = %+v", status.Repository)
	}
	if status.Repository.CapacityState != repository.CapacityAvailable ||
		status.Repository.CapacityTotal == 0 ||
		status.Repository.CapacityFree > status.Repository.CapacityTotal ||
		status.Repository.CapacityUsed != status.Repository.CapacityTotal-status.Repository.CapacityFree ||
		status.Repository.CapacityMeasuredAt == "" || status.Repository.CapacityReason != "" {
		t.Fatalf("repository capacity status = %+v", status.Repository)
	}
	if _, err := time.Parse(time.RFC3339, status.Repository.CapacityMeasuredAt); err != nil {
		t.Fatalf("capacity measured_at = %q: %v", status.Repository.CapacityMeasuredAt, err)
	}
}
