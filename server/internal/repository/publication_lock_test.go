package repository

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPublicationLockIsExclusiveAndReleasedAfterHolderDeath(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestPublicationLockHelperProcess")
	cmd.Env = append(os.Environ(), "RW_PUBLICATION_LOCK_HELPER=1", "RW_PUBLICATION_LOCK_ROOT="+root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	select {
	case line := <-ready:
		if line != "READY" {
			t.Fatalf("helper readiness = %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for lock helper")
	}

	repo, err := OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := repo.AcquirePublicationLock(ctx, "workspace:default"); err == nil {
		t.Fatal("second process acquired an active repository lock")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	takeoverCtx, takeoverCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer takeoverCancel()
	lock, err := repo.AcquirePublicationLock(takeoverCtx, "workspace:default")
	if err != nil {
		t.Fatalf("lock was not released after holder death: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationLockHelperProcess(t *testing.T) {
	if os.Getenv("RW_PUBLICATION_LOCK_HELPER") != "1" {
		return
	}
	repo, err := OpenDir(filepath.Clean(os.Getenv("RW_PUBLICATION_LOCK_ROOT")))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	lock, err := repo.AcquirePublicationLock(context.Background(), "workspace:default")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println("READY")
	time.Sleep(time.Hour)
	_ = lock.Close()
	os.Exit(0)
}
