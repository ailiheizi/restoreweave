package sandbox

import (
	"context"
	"errors"
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
