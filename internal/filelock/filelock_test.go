package filelock

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireSerializesIndependentOwners(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}

	acquired := make(chan *Lock, 1)
	errs := make(chan error, 1)
	go func() {
		second, err := Acquire(path)
		if err != nil {
			errs <- err
			return
		}
		acquired <- second
	}()

	select {
	case second := <-acquired:
		_ = second.Release()
		t.Fatal("second owner acquired while first owner held the lock")
	case err := <-errs:
		t.Fatalf("Acquire(second) error = %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release(first) error = %v", err)
	}

	select {
	case second := <-acquired:
		if err := second.Release(); err != nil {
			t.Fatalf("Release(second) error = %v", err)
		}
	case err := <-errs:
		t.Fatalf("Acquire(second) error = %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("second owner did not acquire after first owner released")
	}
}

func TestOwnershipIsReleasedWhenProcessIsKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.lock")
	command := exec.Command(os.Args[0], "-test.run=TestFileLockHelperProcess", "--", path)
	command.Env = append(os.Environ(), "SQLCERY_FILELOCK_HELPER=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "locked" {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("helper readiness = %q, error = %v", scanner.Text(), scanner.Err())
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("Wait() error = nil, want forced-termination status")
	}

	acquired := make(chan *Lock, 1)
	errs := make(chan error, 1)
	go func() {
		owner, err := Acquire(path)
		if err != nil {
			errs <- err
			return
		}
		acquired <- owner
	}()
	select {
	case owner := <-acquired:
		if err := owner.Release(); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
	case err := <-errs:
		t.Fatalf("Acquire() after forced termination error = %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("lock remained owned after helper process was killed")
	}
}

func TestKilledWaiterDoesNotLeaveStaleOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waiting.lock")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(owner) error = %v", err)
	}
	command := exec.Command(os.Args[0], "-test.run=TestFileLockHelperProcess", "--", path)
	command.Env = append(os.Environ(), "SQLCERY_FILELOCK_HELPER=wait")
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = owner.Release()
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	if err := command.Start(); err != nil {
		_ = owner.Release()
		t.Fatalf("Start() error = %v", err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "waiting" {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = owner.Release()
		t.Fatalf("helper readiness = %q, error = %v", scanner.Text(), scanner.Err())
	}
	time.Sleep(100 * time.Millisecond)
	if err := command.Process.Kill(); err != nil {
		_ = owner.Release()
		t.Fatalf("Kill() error = %v", err)
	}
	if err := command.Wait(); err == nil {
		_ = owner.Release()
		t.Fatal("Wait() error = nil, want forced-termination status")
	}
	if err := owner.Release(); err != nil {
		t.Fatalf("Release(owner) error = %v", err)
	}
	later, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() after killing waiter error = %v", err)
	}
	if err := later.Release(); err != nil {
		t.Fatalf("Release(later) error = %v", err)
	}
}

func TestFileLockHelperProcess(t *testing.T) {
	mode := os.Getenv("SQLCERY_FILELOCK_HELPER")
	if mode != "1" && mode != "wait" {
		return
	}
	path := os.Args[len(os.Args)-1]
	if mode == "wait" {
		fmt.Println("waiting")
	}
	owner, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer owner.Release()
	fmt.Println("locked")
	select {}
}
