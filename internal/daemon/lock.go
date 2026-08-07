package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	ErrLockHeld        = errors.New("daemon lock is held by a live owner")
	ErrUnsafeLockFile  = errors.New("daemon lock path is not a regular file")
	ErrProcessNotFound = errors.New("process not found")
)

// ProcessIdentity identifies the incarnation that owns a daemon lock.
type ProcessIdentity struct {
	PID       int
	StartTime string
}

// ProcessInspector permits deterministic ownership checks without reading a host process table in tests.
type ProcessInspector interface {
	Identity(pid int) (ProcessIdentity, error)
}

type lockRecord struct {
	PID       int    `json:"pid"`
	StartTime string `json:"startTime"`
	Nonce     string `json:"nonce"`
}

// Lock represents the owner-specific right to release a daemon lock.
type Lock struct {
	path   string
	record lockRecord
}

// Nonce identifies this acquisition and is used to prevent foreign-state cleanup.
func (l *Lock) Nonce() string { return l.record.Nonce }

// AcquireLock creates ownership only after proving any prior regular lock is stale.
func AcquireLock(path string, inspector ProcessInspector) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create daemon state directory: %w", err)
	}
	if err := recoverStaleLock(path, inspector); err != nil {
		return nil, err
	}
	self, err := inspector.Identity(os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("verify daemon owner identity: %w", err)
	}
	if self.PID != os.Getpid() || self.StartTime == "" {
		return nil, errors.New("daemon owner identity is incomplete")
	}

	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	record := lockRecord{PID: self.PID, StartTime: self.StartTime, Nonce: nonce}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, ErrLockHeld
		}
		return nil, fmt.Errorf("create daemon lock: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write daemon lock: %w", err)
	}
	return &Lock{path: path, record: record}, nil
}

func recoverStaleLock(path string, inspector ProcessInspector) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect daemon lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafeLockFile
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read daemon lock: %w", err)
	}
	var record lockRecord
	if err := json.Unmarshal(data, &record); err != nil || record.PID <= 0 || record.StartTime == "" || record.Nonce == "" {
		return removeRegularLock(path)
	}
	identity, err := inspector.Identity(record.PID)
	if err == nil && identity.PID == record.PID && identity.StartTime == record.StartTime {
		return ErrLockHeld
	}
	if err != nil && !errors.Is(err, ErrProcessNotFound) {
		return fmt.Errorf("verify daemon lock owner: %w", err)
	}
	return removeRegularLock(path)
}

func removeRegularLock(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafeLockFile
	}
	return os.Remove(path)
}

// Release removes a lock only when its nonce still identifies this owner.
func (l *Lock) Release() error {
	data, err := os.ReadFile(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read daemon lock for release: %w", err)
	}
	var onDisk lockRecord
	if err := json.Unmarshal(data, &onDisk); err != nil || onDisk.Nonce != l.record.Nonce {
		return nil
	}
	return removeRegularLock(l.path)
}

func randomNonce() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("create daemon lock nonce: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
