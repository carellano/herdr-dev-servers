package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDirDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	got, _, err := LoadDir(dir)
	if err != nil || got.Interval().Seconds() != 5 || got.URL.Host != "localhost" {
		t.Fatalf("defaults = %#v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("scan_interval_seconds = 0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadDir(dir); err == nil {
		t.Fatal("invalid interval accepted")
	}
}

func TestLoadDirRejectsUnknownAndAcceptsPolicies(t *testing.T) {
	dir := t.TempDir()
	data := "scan_interval_seconds=10\nshow_external=true\ngrouping=\"process-ancestry\"\nsidebar_min=20\nsidebar_max=40\nignored_ports=[3000]\nopener=\"disabled\"\nclipboard=\"system\"\n[url]\nscheme=\"https\"\nhost=\"localhost\"\npath=\"/app\"\n"
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadDir(dir)
	if err != nil || !got.ShowExternal || !got.Ignored(3000) {
		t.Fatalf("load = %#v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("unknown=true"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadDir(dir); err == nil {
		t.Fatal("unknown key accepted")
	}
}
