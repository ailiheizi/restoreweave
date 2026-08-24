package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildArgvIsolatesWorkerAndStaging(t *testing.T) {
	staging := t.TempDir()
	binary := filepath.Join(staging, "worker")
	argv, err := BuildArgv(Spec{
		Binary:     binary,
		Args:       []string{"--capability", "extract.text.v1"},
		StagingDir: staging,
		Env:        map[string]string{"PATH": "/worker", "LANG": "C"},
	})
	if err != nil {
		t.Fatalf("build argv: %v", err)
	}
	joined := strings.Join(argv, " ")
	for _, flag := range []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-net",
		"--unshare-pid",
		"--clearenv",
		"--ro-bind " + binary + " /worker",
		"--bind " + staging + " /stage",
		"--chdir /stage",
		"--setenv LANG C",
		"--setenv PATH /worker",
		"/worker --capability extract.text.v1",
	} {
		if !strings.Contains(joined, flag) {
			t.Fatalf("argv missing %q:\n%s", flag, joined)
		}
	}
	for _, forbidden := range []string{"--share-net", "--unshare-net=false", "/source", "--cap-add"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("argv contains forbidden %q:\n%s", forbidden, joined)
		}
	}
}

func TestValidateRejectsNetworkAndExtraBinds(t *testing.T) {
	staging := t.TempDir()
	binary := filepath.Join(staging, "worker")
	if err := (Spec{Binary: binary, StagingDir: staging, Network: true}).Validate(); !errors.Is(err, ErrNetworkRequested) {
		t.Fatalf("network = %v, want ErrNetworkRequested", err)
	}
	if err := (Spec{
		Binary:     binary,
		StagingDir: staging,
		ExtraBinds: []Bind{{Host: "/media", Dest: "/source"}},
	}).Validate(); !errors.Is(err, ErrExtraBinds) {
		t.Fatalf("extra binds = %v, want ErrExtraBinds", err)
	}
	if err := (Spec{Binary: binary, StagingDir: staging, Env: map[string]string{"LD_PRELOAD": "x"}}).Validate(); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("ld_preload = %v, want ErrInvalidSpec", err)
	}
	if _, err := BuildArgv(Spec{StagingDir: staging}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("missing binary = %v", err)
	}
	if _, err := BuildArgv(Spec{Binary: binary, StagingDir: staging, PreserveFDs: []int{4}}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("arbitrary preserved fd = %v", err)
	}
}

func TestBuildArgvSupportsReadOnlyStagingAndNonceFD(t *testing.T) {
	staging := t.TempDir()
	binary := filepath.Join(staging, "worker")
	argv, err := BuildArgv(Spec{Binary: binary, StagingDir: staging, ReadOnlyStaging: true, PreserveFDs: []int{3}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--ro-bind "+staging+" /stage") || !strings.Contains(joined, "--preserve-fd 3") {
		t.Fatalf("read-only staging/nonce missing: %s", joined)
	}
	if _, err := os.Stat("/bin"); err == nil && !strings.Contains(joined, "--ro-bind /bin /bin") {
		t.Fatalf("read-only system dependency mount missing: %s", joined)
	}
}

func TestBuildArgvSupportsNonceFileFallback(t *testing.T) {
	staging := t.TempDir()
	argv, err := BuildArgv(Spec{Binary: filepath.Join(staging, "worker"), StagingDir: staging, ReadOnlyStaging: true, NonceFilePath: noncePath})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--file 3 "+noncePath) || strings.Contains(joined, "--preserve-fd") {
		t.Fatalf("nonce file fallback argv = %s", joined)
	}
	if _, err := BuildArgv(Spec{Binary: filepath.Join(staging, "worker"), StagingDir: staging, NonceFilePath: "/tmp/nonce"}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("invalid nonce path error = %v, want ErrInvalidSpec", err)
	}
}

func TestRunRespectsPlatform(t *testing.T) {
	staging := t.TempDir()
	spec := Spec{Binary: filepath.Join(staging, "worker"), StagingDir: staging}
	if !Supported() {
		err := Run(context.Background(), spec)
		if !errors.Is(err, ErrUnsupportedPlatform) {
			t.Fatalf("non-linux run = %v, want unsupported platform", err)
		}
		return
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		if err := Run(context.Background(), spec); !errors.Is(err, ErrBubblewrapMissing) {
			t.Fatalf("linux without bwrap = %v, want ErrBubblewrapMissing", err)
		}
	}
}
