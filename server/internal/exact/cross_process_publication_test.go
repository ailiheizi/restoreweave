package exact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/capture"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

const (
	crossProcessPublicationHelper = "RW_EXACT_PUBLICATION_HELPER"
	crossProcessSnapshotRef       = "cross-process-snapshot"
)

func TestSignedPublicationCrossProcessFenceSingleGeneration(t *testing.T) {
	repositoryRoot := filepath.Join(t.TempDir(), "repository")
	if _, err := repository.OpenDir(repositoryRoot); err != nil {
		t.Fatal(err)
	}
	signingRoot := filepath.Join(t.TempDir(), "signing")
	_, anchor, err := OpenSigningMaterial(signingRoot, testPublicationDomain, true)
	if err != nil {
		t.Fatal(err)
	}
	coordinationRoot := t.TempDir()
	startPath := filepath.Join(coordinationRoot, "start")
	planDigest := DigestBytes([]byte("cross-process-publication-plan"))

	type child struct {
		name   string
		cmd    *exec.Cmd
		stdout bytes.Buffer
		stderr bytes.Buffer
	}
	children := []*child{{name: "writer-a"}, {name: "writer-b"}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, candidate := range children {
		candidate.cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSignedPublicationCrossProcessHelper$")
		candidate.cmd.Env = append(os.Environ(),
			crossProcessPublicationHelper+"=1",
			"RW_EXACT_PUBLICATION_REPOSITORY="+repositoryRoot,
			"RW_EXACT_PUBLICATION_SIGNING="+signingRoot,
			"RW_EXACT_PUBLICATION_COORDINATION="+coordinationRoot,
			"RW_EXACT_PUBLICATION_CANDIDATE="+candidate.name,
			"RW_EXACT_PUBLICATION_PLAN="+planDigest,
		)
		candidate.cmd.Stdout = &candidate.stdout
		candidate.cmd.Stderr = &candidate.stderr
		if err := candidate.cmd.Start(); err != nil {
			t.Fatalf("start %s: %v", candidate.name, err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for _, candidate := range children {
		readyPath := filepath.Join(coordinationRoot, candidate.name+".ready")
		for {
			if _, err := os.Stat(readyPath); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("observe %s readiness: %v", candidate.name, err)
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s readiness", candidate.name)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := os.WriteFile(startPath, []byte("start\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, candidate := range children {
		if err := candidate.cmd.Wait(); err != nil {
			t.Fatalf("%s failed: %v\nstdout:\n%s\nstderr:\n%s", candidate.name, err, candidate.stdout.String(), candidate.stderr.String())
		}
	}
	published, rejected := 0, 0
	for _, candidate := range children {
		switch {
		case strings.Contains(candidate.stdout.String(), "PUBLISHED "+candidate.name):
			published++
		case strings.Contains(candidate.stdout.String(), "STALE "+candidate.name):
			rejected++
		default:
			t.Fatalf("%s returned no publication outcome: stdout=%q stderr=%q", candidate.name, candidate.stdout.String(), candidate.stderr.String())
		}
	}
	if published != 1 || rejected != 1 {
		t.Fatalf("cross-process outcomes = published %d stale %d, want 1/1", published, rejected)
	}

	repo, err := repository.OpenDir(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	reader := &Service{
		Repo: repo, TrustAnchor: &anchor, PublicationDomain: testPublicationDomain,
		RequireSignedPublication: true,
	}
	publications, err := reader.committedPublications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(publications) != 1 || publications[0].Commit.Generation != 1 ||
		publications[0].Commit.FenceToken != 1 || publications[0].Prepared.Prepared.FenceToken != 1 {
		t.Fatalf("cross-process publication lineage = %+v, want one fenced genesis", publications)
	}
	for _, role := range []repository.RecordRole{repository.RecordPreparedClosure, repository.RecordPublicationCommit} {
		digests, err := repo.ListRecordDigests(context.Background(), role)
		if err != nil {
			t.Fatal(err)
		}
		if len(digests) != 1 {
			t.Fatalf("%s records = %d, want one", role, len(digests))
		}
	}
}

func TestSignedPublicationCrossProcessHelper(t *testing.T) {
	if os.Getenv(crossProcessPublicationHelper) != "1" {
		return
	}
	repositoryRoot := filepath.Clean(os.Getenv("RW_EXACT_PUBLICATION_REPOSITORY"))
	signingRoot := filepath.Clean(os.Getenv("RW_EXACT_PUBLICATION_SIGNING"))
	coordinationRoot := filepath.Clean(os.Getenv("RW_EXACT_PUBLICATION_COORDINATION"))
	candidate := os.Getenv("RW_EXACT_PUBLICATION_CANDIDATE")
	planDigest := os.Getenv("RW_EXACT_PUBLICATION_PLAN")
	if repositoryRoot == "." || signingRoot == "." || coordinationRoot == "." || candidate == "" || planDigest == "" {
		fmt.Fprintln(os.Stderr, "cross-process publication helper configuration is incomplete")
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(coordinationRoot, candidate+".ready"), []byte("ready\n"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	startPath := filepath.Join(coordinationRoot, "start")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(startPath); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "timed out waiting for publication start")
			os.Exit(2)
		}
		time.Sleep(10 * time.Millisecond)
	}

	repo, err := repository.OpenDir(repositoryRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	identity, anchor, err := OpenSigningMaterial(signingRoot, testPublicationDomain, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	manifest, err := writeManifest(repo.Root(), Manifest{
		Schema: SnapshotSchemaV1, SnapshotRef: crossProcessSnapshotRef,
		CreatedAt: time.Unix(1, 0).UTC(),
		Binding: capture.BindingRecord{
			Schema: capture.SchemaBindingV1, Profile: capture.ProfileLocalTree,
			CaptureMode: "ROOTED_FD", BoundAt: time.Unix(1, 0).UTC(),
		},
		Entries: []ManifestEntry{},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	service := &Service{
		Repo: repo, SigningIdentity: &identity, TrustAnchor: &anchor,
		PublicationDomain: testPublicationDomain, RequireSignedPublication: true,
	}
	_, err = service.publishRecoveryClosure(context.Background(), adopted{
		snapshotRef: crossProcessSnapshotRef, publicationID: "publication:" + candidate,
	}, manifest, placedSet{}, planDigest, DigestBytes([]byte("cross-process-capture")), DigestBytes([]byte("cross-process-policy")))
	switch {
	case err == nil:
		fmt.Println("PUBLISHED " + candidate)
	case errors.Is(err, ErrPublicationAlreadyCommitted):
		fmt.Println("STALE " + candidate)
	default:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
