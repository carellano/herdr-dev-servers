package proof

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const darwinUnixSocketLimit = 104

// DarwinHostSafety confines an opt-in proof adapter to one temporary root.
// It only records resources it creates, so cleanup cannot affect host state.
type DarwinHostSafety struct {
	root  string
	owned map[string]struct{}
}

// NewDarwinHostSafety creates a short isolated root from a caller-supplied temporary parent.
func NewDarwinHostSafety(parent string) (*DarwinHostSafety, error) {
	parent, err := filepath.Abs(parent)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve temporary parent", ErrLiveSafety)
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: temporary parent is not a real directory", ErrLiveSafety)
	}
	root, err := os.MkdirTemp(parent, "ha-")
	if err != nil {
		return nil, fmt.Errorf("%w: create isolated temporary root", ErrLiveSafety)
	}
	if host, err := validateDarwinHostRoot(root); err == nil {
		return host, nil
	}
	_ = os.Remove(root)
	root, err = os.MkdirTemp("", "ha-")
	if err != nil {
		return nil, fmt.Errorf("%w: create short isolated temporary root", ErrLiveSafety)
	}
	host, err := validateDarwinHostRoot(root)
	if err != nil {
		_ = os.Remove(root)
	}
	return host, err
}

func validateDarwinHostRoot(root string) (*DarwinHostSafety, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || len([]byte(filepath.Join(root, "fake.sock"))) >= darwinUnixSocketLimit {
		return nil, fmt.Errorf("%w: isolated root is unsafe", ErrLiveSafety)
	}
	return &DarwinHostSafety{root: root, owned: map[string]struct{}{}}, nil
}

func (h *DarwinHostSafety) Root() string { return h.root }

// FakeEndpoint reserves a direct child endpoint for the fake Herdr server.
func (h *DarwinHostSafety) FakeEndpoint(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%w: fake endpoint must be a plain filename", ErrLiveSafety)
	}
	path := filepath.Join(h.root, name)
	if len([]byte(path)) >= darwinUnixSocketLimit {
		return "", fmt.Errorf("%w: fake endpoint exceeds Darwin Unix socket limit", ErrLiveSafety)
	}
	h.owned[path] = struct{}{}
	return path, nil
}

// FixedArgv preserves argument boundaries for exec.Command without a shell.
func (h *DarwinHostSafety) FixedArgv(binary string, args ...string) ([]string, error) {
	if binary == "" || strings.ContainsAny(binary, "\x00\t\n ") {
		return nil, fmt.Errorf("%w: executable must be one literal argv element", ErrLiveSafety)
	}
	argv := make([]string, 1, len(args)+1)
	argv[0] = binary
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return nil, fmt.Errorf("%w: argv contains NUL", ErrLiveSafety)
		}
		argv = append(argv, arg)
	}
	return argv, nil
}

func (h *DarwinHostSafety) Owns(path string) bool {
	_, ok := h.owned[path]
	return ok
}

// RemoveOwned removes only a path that was reserved by FakeEndpoint.
func (h *DarwinHostSafety) RemoveOwned(path string) error {
	if !h.Owns(path) {
		return fmt.Errorf("%w: refuse cleanup of unowned resource", ErrLiveSafety)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: remove owned resource", ErrLiveSafety)
	}
	return nil
}

// Cleanup refuses to remove the root while unowned entries are present.
func (h *DarwinHostSafety) Cleanup() error {
	entries, err := os.ReadDir(h.root)
	if err != nil {
		return fmt.Errorf("%w: inspect temporary root", ErrLiveSafety)
	}
	for _, entry := range entries {
		if !h.Owns(filepath.Join(h.root, entry.Name())) {
			return fmt.Errorf("%w: temporary root contains unowned resource", ErrLiveSafety)
		}
	}
	for path := range h.owned {
		if err := h.RemoveOwned(path); err != nil {
			return err
		}
	}
	if err := os.Remove(h.root); err != nil {
		return fmt.Errorf("%w: remove isolated temporary root", ErrLiveSafety)
	}
	return nil
}
