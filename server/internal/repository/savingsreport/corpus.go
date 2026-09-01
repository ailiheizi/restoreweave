//go:build savingsreport

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

const corpusManifestSchema = "restoreweave.corpus-manifest.v1"

// CorpusEntry is the only input fact used by the savings runner. Path is a
// slash-separated path relative to the corpus root, never an absolute host
// path. The manifest is deliberately small and deterministic so two profiles
// can be compared against precisely the same bytes.
type CorpusEntry struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// CorpusManifest is a canonical, content-addressed description of a corpus.
// Digest is SHA-256 over the canonical JSON with Digest omitted. It is not a
// release or qualification claim; it binds one candidate measurement to one
// observed input set.
type CorpusManifest struct {
	Schema  string        `json:"schema"`
	Entries []CorpusEntry `json:"entries"`
	Digest  string        `json:"digest"`
}

type corpusManifestPayload struct {
	Schema  string        `json:"schema"`
	Entries []CorpusEntry `json:"entries"`
}

// CandidateEvidence is an optional machine-readable measurement record. The
// status intentionally says candidate: this runner must never imply that a
// profile is the release/default engine.
type CandidateEvidence struct {
	Schema               string                        `json:"schema"`
	Status               string                        `json:"status"`
	GeneratedAt          string                        `json:"generated_at"`
	OS                   string                        `json:"os"`
	Arch                 string                        `json:"arch"`
	Profile              repository.ProfileDescription `json:"profile"`
	Capabilities         repository.CapabilityProfile  `json:"capabilities"`
	Corpus               CorpusManifest                `json:"corpus"`
	Report               repository.SavingsReport      `json:"report"`
	DurationMilliseconds int64                         `json:"duration_ms"`
	Unmeasured           []string                      `json:"unmeasured"`
	MeasurementNotes     map[string]string             `json:"measurement_notes"`
}

// BuildCorpusManifest scans an existing, non-empty directory without
// creating or changing anything in it. Symlinks, special files, and path
// anomalies are rejected rather than silently omitted.
func BuildCorpusManifest(root string) (CorpusManifest, error) {
	if strings.TrimSpace(root) == "" {
		return CorpusManifest{}, errors.New("corpus directory is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return CorpusManifest{}, fmt.Errorf("resolve corpus directory: %w", err)
	}
	rootInfo, err := os.Lstat(absolute)
	if err != nil {
		return CorpusManifest{}, fmt.Errorf("stat corpus directory: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return CorpusManifest{}, errors.New("corpus directory must not be a symlink")
	}
	if !rootInfo.IsDir() {
		return CorpusManifest{}, errors.New("corpus path is not a directory")
	}

	entries := make([]CorpusEntry, 0)
	err = filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == absolute {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("corpus contains symlink %q", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("corpus contains non-regular file %q", path)
		}
		relative, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." || relative == "" || strings.HasPrefix(relative, "../") || strings.Contains(relative, "/../") {
			return fmt.Errorf("corpus contains unsafe relative path %q", relative)
		}
		sha, size, err := hashFile(absolute, path)
		if err != nil {
			return fmt.Errorf("hash corpus file %q: %w", relative, err)
		}
		entries = append(entries, CorpusEntry{Path: relative, Bytes: size, SHA256: sha})
		return nil
	})
	if err != nil {
		return CorpusManifest{}, err
	}
	if len(entries) == 0 {
		return CorpusManifest{}, errors.New("corpus directory must contain at least one regular file")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	manifest := CorpusManifest{Schema: corpusManifestSchema, Entries: entries}
	digest, err := manifestDigest(manifest)
	if err != nil {
		return CorpusManifest{}, err
	}
	manifest.Digest = digest
	return manifest, nil
}

func hashFile(root, path string) (string, int64, error) {
	file, err := openCorpusFile(root, path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

// openCorpusFile opens one corpus member and verifies the opened handle is
// still the regular object named by the corpus path. The before/after real
// path checks catch parent-directory symlink redirection, while SameFile
// catches a final-component swap. If the path is replaced after this point,
// the already-open handle continues to reference the checked object.
func openCorpusFile(root, path string) (*os.File, error) {
	if err := ensurePathWithinRoot(root, path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	closeOnError := func(openErr error) (*os.File, error) {
		_ = file.Close()
		return nil, openErr
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return closeOnError(err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return closeOnError(err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() ||
		!openedInfo.Mode().IsRegular() || !os.SameFile(openedInfo, pathInfo) {
		return closeOnError(errors.New("corpus file changed to a different or non-regular object"))
	}
	if err := ensurePathWithinRoot(root, path); err != nil {
		return closeOnError(err)
	}
	return file, nil
}

func ensurePathWithinRoot(root, path string) error {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve corpus root: %w", err)
	}
	pathReal, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve corpus member: %w", err)
	}
	if !sameOrNested(rootReal, pathReal) {
		return fmt.Errorf("corpus member resolves outside corpus root: %q", path)
	}
	return nil
}

func manifestDigest(manifest CorpusManifest) (string, error) {
	payload := corpusManifestPayload{Schema: corpusManifestSchema, Entries: manifest.Entries}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func manifestEqual(a, b CorpusManifest) bool {
	return a.Schema == b.Schema && a.Digest == b.Digest && len(a.Entries) == len(b.Entries) &&
		entriesEqual(a.Entries, b.Entries)
}

func entriesEqual(a, b []CorpusEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeJSONFile(path string, value any) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".savings-report-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func validateWorkDir(corpus, work string) error {
	if strings.TrimSpace(work) == "" {
		return errors.New("work directory is required")
	}
	workAbs, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	overlap, err := pathsOverlap(corpus, work)
	if err != nil {
		return err
	}
	if overlap {
		return errors.New("work directory and corpus directory must be separate and non-nested")
	}
	if info, err := os.Lstat(workAbs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("work directory must not be a symlink")
		}
		if !info.IsDir() {
			return errors.New("work path is not a directory")
		}
		children, err := os.ReadDir(workAbs)
		if err != nil {
			return err
		}
		if len(children) != 0 {
			return errors.New("work directory must be empty")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

// pathsOverlap uses both lexical containment and filesystem identity. The
// identity walk handles a missing work/output leaf below a symlinked parent
// and case aliases on case-insensitive filesystems.
func pathsOverlap(a, b string) (bool, error) {
	aAbs, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	bAbs, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}
	if sameOrNested(aAbs, bAbs) || sameOrNested(bAbs, aAbs) {
		return true, nil
	}
	aInfo, aErr := os.Stat(aAbs)
	if aErr != nil && !os.IsNotExist(aErr) {
		return false, aErr
	}
	bInfo, bErr := os.Stat(bAbs)
	if bErr != nil && !os.IsNotExist(bErr) {
		return false, bErr
	}
	if aErr == nil {
		match, err := ancestorHasFile(bAbs, aInfo)
		if err != nil || match {
			return match, err
		}
	}
	if bErr == nil {
		match, err := ancestorHasFile(aAbs, bInfo)
		if err != nil || match {
			return match, err
		}
	}
	return false, nil
}

func ancestorHasFile(path string, want os.FileInfo) (bool, error) {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if info, err := os.Stat(current); err == nil {
			if os.SameFile(info, want) {
				return true, nil
			}
		} else if !os.IsNotExist(err) {
			return false, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
	}
}

func sameOrNested(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func candidateEvidence(manifest CorpusManifest, driver repository.Driver, profile repository.ProfileDescription, report repository.SavingsReport, started time.Time) CandidateEvidence {
	// The runner owns only the repository root; the operational catalog is an
	// external dependency and must remain visibly unmeasured in candidate
	// evidence rather than looking like a measured zero.
	unmeasured := []string{"catalog"}
	for name, category := range map[string]repository.SavingsOverheadCategory{
		"index":     report.Overhead.Index,
		"model":     report.Overhead.Model,
		"temporary": report.Overhead.Temporary,
	} {
		if category.Status == repository.SavingsCategoryUnmeasured {
			unmeasured = append(unmeasured, name)
		}
	}
	sort.Strings(unmeasured)
	capabilities := repository.CapabilityProfile{}
	if reporter, ok := driver.(repository.CapabilityReporter); ok {
		capabilities = reporter.DescribeCapabilities()
	}
	return CandidateEvidence{
		Schema:               "restoreweave.storage-savings-candidate.v1",
		Status:               "CANDIDATE_MEASUREMENT_ONLY",
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		OS:                   runtime.GOOS,
		Arch:                 runtime.GOARCH,
		Profile:              profile,
		Capabilities:         capabilities,
		Corpus:               manifest,
		Report:               report,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		Unmeasured:           unmeasured,
		MeasurementNotes: map[string]string{
			"identity":       "sha256+length",
			"deduplication":  "whole-file-exact",
			"recovery":       "readback-verified",
			"catalog":        "UNMEASURED_OUTSIDE_REPOSITORY_BOUNDARY",
			"destructive_gc": "NOT_MEASURED_AND_NOT_ENABLED",
		},
	}
}
