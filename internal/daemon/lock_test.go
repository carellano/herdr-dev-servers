package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeLockFile(t *testing.T, path string, record lockRecord) {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertLockRecord(t *testing.T, path string, want lockRecord) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got lockRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("lock record = %#v, want %#v", got, want)
	}
}

type fakeInspector struct {
	identities map[int]ProcessIdentity
	err        error
}

func (f fakeInspector) Identity(pid int) (ProcessIdentity, error) {
	if f.err != nil {
		return ProcessIdentity{}, f.err
	}
	identity, ok := f.identities[pid]
	if !ok {
		if pid == os.Getpid() {
			return ProcessIdentity{PID: pid, StartTime: "self"}, nil
		}
		return ProcessIdentity{}, ErrProcessNotFound
	}
	return identity, nil
}

func TestAcquireLockPreservesLiveOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	writeLockFile(t, path, lockRecord{PID: 41, StartTime: "live", Nonce: "other"})

	_, err := AcquireLock(path, fakeInspector{identities: map[int]ProcessIdentity{41: {PID: 41, StartTime: "live"}}})
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("AcquireLock() error = %v, want ErrLockHeld", err)
	}
	assertLockRecord(t, path, lockRecord{PID: 41, StartTime: "live", Nonce: "other"})
}

func TestAcquireLockRecoversReusedPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	writeLockFile(t, path, lockRecord{PID: 41, StartTime: "old", Nonce: "stale"})

	lock, err := AcquireLock(path, fakeInspector{identities: map[int]ProcessIdentity{41: {PID: 41, StartTime: "new"}}})
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if lock.Nonce() == "stale" {
		t.Fatal("AcquireLock() retained stale owner nonce")
	}
}

func TestAcquireLockRecoversCorruptRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := AcquireLock(path, fakeInspector{}); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
}

func TestAcquireLockPreservesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "foreign.lock")
	path := filepath.Join(dir, "daemon.lock")
	if err := os.WriteFile(target, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	_, err := AcquireLock(path, fakeInspector{})
	if !errors.Is(err, ErrUnsafeLockFile) {
		t.Fatalf("AcquireLock() error = %v, want ErrUnsafeLockFile", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("AcquireLock() removed symlink: %v", err)
	}
}

func TestReleasePreservesForeignNonce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock := &Lock{path: path, record: lockRecord{PID: 1, StartTime: "mine", Nonce: "mine"}}
	writeLockFile(t, path, lockRecord{PID: 2, StartTime: "foreign", Nonce: "foreign"})

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	assertLockRecord(t, path, lockRecord{PID: 2, StartTime: "foreign", Nonce: "foreign"})
}
