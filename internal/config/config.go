// Package config loads only plugin-owned configuration from an explicit directory.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const fileName = "config.toml"

type Config struct {
	ScanIntervalSeconds int    `toml:"scan_interval_seconds"`
	ShowExternal        bool   `toml:"show_external"`
	Grouping            string `toml:"grouping"`
	SidebarMin          int    `toml:"sidebar_min"`
	SidebarMax          int    `toml:"sidebar_max"`
	URL                 URL    `toml:"url"`
	IgnoredPorts        []int  `toml:"ignored_ports"`
	Opener              string `toml:"opener"`
	Clipboard           string `toml:"clipboard"`
}

type URL struct {
	Scheme string `toml:"scheme"`
	Host   string `toml:"host"`
	Path   string `toml:"path"`
}

func Defaults() Config {
	return Config{ScanIntervalSeconds: 5, Grouping: "process-ancestry", SidebarMin: 24, SidebarMax: 48, URL: URL{Scheme: "http", Host: "localhost", Path: ""}, Opener: "system", Clipboard: "system"}
}

func Dir() (string, error) {
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve plugin config directory: %w", err)
	}
	return filepath.Join(base, "herdr", "plugins", "apps"), nil
}

func Load() (Config, string, error) {
	dir, err := Dir()
	if err != nil {
		return Config{}, "", err
	}
	return LoadDir(dir)
}

func LoadDir(dir string) (Config, string, error) {
	cfg := Defaults()
	path := filepath.Join(dir, fileName)
	metadata, err := toml.DecodeFile(path, &cfg)
	if os.IsNotExist(err) {
		return cfg, path, nil
	}
	if err != nil {
		return Config{}, path, fmt.Errorf("parse %s: %w", path, err)
	}
	if keys := metadata.Undecoded(); len(keys) > 0 {
		return Config{}, path, fmt.Errorf("unknown config keys: %s", keys)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, path, err
	}
	return cfg, path, nil
}

func (c Config) Interval() time.Duration { return time.Duration(c.ScanIntervalSeconds) * time.Second }

func (c Config) Validate() error {
	if c.ScanIntervalSeconds < 1 || c.ScanIntervalSeconds > 3600 {
		return fmt.Errorf("scan_interval_seconds must be 1..3600")
	}
	if c.Grouping != "process-ancestry" {
		return fmt.Errorf("grouping must be process-ancestry")
	}
	if c.SidebarMin < 16 || c.SidebarMax < c.SidebarMin || c.SidebarMax > 120 {
		return fmt.Errorf("sidebar bounds must be 16 <= min <= max <= 120")
	}
	if c.URL.Scheme != "http" && c.URL.Scheme != "https" {
		return fmt.Errorf("url.scheme must be http or https")
	}
	if c.URL.Host != "localhost" && c.URL.Host != "127.0.0.1" && c.URL.Host != "::1" {
		return fmt.Errorf("url.host must be loopback")
	}
	if strings.ContainsAny(c.URL.Path, "\r\n") || (c.URL.Path != "" && !strings.HasPrefix(c.URL.Path, "/")) {
		return fmt.Errorf("url.path must be empty or start with /")
	}
	if c.Opener != "system" && c.Opener != "disabled" {
		return fmt.Errorf("opener must be system or disabled")
	}
	if c.Clipboard != "system" && c.Clipboard != "disabled" {
		return fmt.Errorf("clipboard must be system or disabled")
	}
	seen := map[int]bool{}
	for _, port := range c.IgnoredPorts {
		if port < 1 || port > 65535 || seen[port] {
			return fmt.Errorf("ignored_ports must contain unique TCP ports")
		}
		seen[port] = true
	}
	return nil
}

func (c Config) Ignored(port int) bool {
	return sort.SearchInts(sorted(c.IgnoredPorts), port) < len(c.IgnoredPorts) && sorted(c.IgnoredPorts)[sort.SearchInts(sorted(c.IgnoredPorts), port)] == port
}
func sorted(values []int) []int { copy := append([]int(nil), values...); sort.Ints(copy); return copy }
