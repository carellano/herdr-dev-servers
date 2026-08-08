package proof

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinHostSafetyCreatesIsolatedRootAndOwnedFakeEndpoint(t *testing.T) {
	host, err := NewDarwinHostSafety(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if host.Root() == "" {
		t.Fatal("root is empty")
	}
	endpoint, err := host.FakeEndpoint("herdr.sock")
	if err != nil {
		t.Fatal(err)
	}
	if !host.Owns(endpoint) || filepath.Dir(endpoint) != host.Root() {
		t.Fatalf("endpoint %q is not an owned root child", endpoint)
	}
	if err := os.WriteFile(endpoint, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := host.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(host.Root()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root remained after cleanup: %v", err)
	}
}

func TestDarwinHostSafetyFailsClosed(t *testing.T) {
	parent := t.TempDir()
	longParent := filepath.Join(parent, strings.Repeat("x", 100))
	if err := os.Mkdir(longParent, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		run  func(*DarwinHostSafety) error
	}{
		{"rejects socket path at Unix limit", func(host *DarwinHostSafety) error { _, err := host.FakeEndpoint("fake.sock"); return err }},
		{"rejects traversal endpoint", func(host *DarwinHostSafety) error { _, err := host.FakeEndpoint("../foreign.sock"); return err }},
		{"rejects shell-like executable", func(host *DarwinHostSafety) error { _, err := host.FixedArgv("/bin/sh -c", "echo unsafe"); return err }},
		{"rejects unowned cleanup", func(host *DarwinHostSafety) error { return host.RemoveOwned(filepath.Join(parent, "foreign")) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := parent
			if tt.name == "rejects socket path at Unix limit" {
				root = longParent
			}
			if tt.name == "rejects socket path at Unix limit" {
				if _, err := validateDarwinHostRoot(root); !errors.Is(err, ErrLiveSafety) {
					t.Fatalf("validation error = %v, want ErrLiveSafety", err)
				}
				return
			}
			host, err := NewDarwinHostSafety(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.run(host); !errors.Is(err, ErrLiveSafety) {
				t.Fatalf("error = %v, want ErrLiveSafety", err)
			}
		})
	}
}

func TestDarwinHostSafetyBuildsLiteralArgvAndPreservesForeignFiles(t *testing.T) {
	host, err := NewDarwinHostSafety(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	argv, err := host.FixedArgv("/usr/bin/lsof", "-nP", "-iTCP -sTCP:LISTEN")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/lsof", "-nP", "-iTCP -sTCP:LISTEN"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", argv, want)
	}
	foreign := filepath.Join(host.Root(), "foreign")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := host.Cleanup(); !errors.Is(err, ErrLiveSafety) {
		t.Fatalf("Cleanup() error = %v, want ErrLiveSafety", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign file was removed: %v", err)
	}
}
