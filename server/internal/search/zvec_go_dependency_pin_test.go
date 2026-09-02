//go:build purego

package search

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	zvec "github.com/zvec-ai/zvec-go"
)

// Keep a real reference to the linked binding so linker dead-code pruning
// cannot hide it from BuildInfo. Merely taking this function value does not
// initialize or load the native zvec library.
var zvecDependencyProbe = zvec.GetVersion

// TestZvecGoBuildDependencyMatchesBundlePin proves that the zvec-go package
// linked into this purego test binary is the immutable pseudo-version used by
// the semantic bundle descriptor.  A mutable release tag or an unpinned
// replacement must not satisfy this check.
func TestZvecGoBuildDependencyMatchesBundlePin(t *testing.T) {
	if zvecDependencyProbe == nil {
		t.Fatal("zvec-go dependency probe is unexpectedly nil")
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("runtime build information is unavailable")
	}
	const modulePath = "github.com/zvec-ai/zvec-go"
	dependency := findBuildDependency(info, modulePath)
	if dependency == nil {
		// Go test binaries do not retain their module dependency list in
		// ReadBuildInfo. Build the actual daemon (the purego consumer) and
		// inspect its executable BuildInfo instead; this still avoids running
		// the daemon and therefore cannot initialize native zvec.
		moduleRoot := moduleRootForDependencyTest(t)
		binary := filepath.Join(t.TempDir(), "restoreweaved")
		build := exec.Command("go", "build", "-tags=purego", "-o", binary, "./server/cmd/restoreweaved")
		build.Dir = moduleRoot
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build purego daemon for dependency provenance: %v\n%s", err, output)
		}
		binaryInfo, err := buildinfo.ReadFile(binary)
		if err != nil {
			t.Fatalf("read purego daemon BuildInfo: %v", err)
		}
		dependency = findBuildDependency(binaryInfo, modulePath)
	}
	if dependency == nil {
		t.Fatalf("build dependency %q is missing", modulePath)
	}
	if dependency.Version != semanticInstallerZvecGoModuleVersion {
		t.Fatalf("linked zvec-go version = %q, want immutable pin %q", dependency.Version, semanticInstallerZvecGoModuleVersion)
	}
	if dependency.Replace != nil {
		t.Fatalf("linked zvec-go dependency is replaced by %q; the immutable module pin must be built directly", dependency.Replace.Path)
	}

	// Go pseudo-versions retain the first 12 hexadecimal characters of the
	// source commit. Parse that suffix from the installer pin so the test has
	// one version source of truth, then compare it with the full descriptor
	// commit.
	versionCommitSeparator := strings.LastIndexByte(semanticInstallerZvecGoModuleVersion, '-')
	if versionCommitSeparator < 0 || versionCommitSeparator == len(semanticInstallerZvecGoModuleVersion)-1 {
		t.Fatalf("installer zvec-go version %q has no pseudo-version commit", semanticInstallerZvecGoModuleVersion)
	}
	pseudoCommit := semanticInstallerZvecGoModuleVersion[versionCommitSeparator+1:]
	if len(pseudoCommit) != 12 || !strings.HasPrefix(semanticInstallerZvecCommit, pseudoCommit) {
		t.Fatalf("linked zvec-go pseudo-version %q does not identify descriptor commit %q", dependency.Version, semanticInstallerZvecCommit)
	}
}

func findBuildDependency(info *debug.BuildInfo, modulePath string) *debug.Module {
	if info == nil {
		return nil
	}
	for _, candidate := range info.Deps {
		if candidate != nil && candidate.Path == modulePath {
			return candidate
		}
	}
	return nil
}

func moduleRootForDependencyTest(t *testing.T) string {
	t.Helper()
	moduleFile, goEnvErr := exec.Command("go", "env", "GOMOD").Output()
	if goEnvErr == nil {
		if path := strings.TrimSpace(string(moduleFile)); path != "" && path != os.DevNull {
			if absolute, absErr := filepath.Abs(path); absErr == nil {
				if info, statErr := os.Stat(absolute); statErr == nil && !info.IsDir() {
					return filepath.Dir(absolute)
				}
			}
		}
	}
	// Keep the test useful in restricted build environments where `go env`
	// cannot execute. Walk from the process working directory instead of using
	// runtime.Caller, whose path is intentionally rewritten by -trimpath.
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("locate module root: go env GOMOD failed: %v; getwd failed: %v", goEnvErr, err)
	}
	for current := filepath.Clean(workingDir); ; current = filepath.Dir(current) {
		candidate := filepath.Join(current, "go.mod")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	t.Fatalf("locate module root: go env GOMOD failed (%v) and no go.mod found from %q", goEnvErr, workingDir)
	return ""
}
