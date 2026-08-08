// Package config loads only plugin-owned configuration from an explicit directory.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

const fileName = "config.toml"

type Config struct {
	ScanIntervalSeconds int    `toml:"scan_interval_seconds"`
	IgnoredPorts        []int  `toml:"ignored_ports"`
	Opener              string `toml:"opener"`
	Clipboard           string `toml:"clipboard"`
}

func Defaults() Config {
	return Config{ScanIntervalSeconds: 5, Opener: "system", Clipboard: "system"}
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
	values := sorted(c.IgnoredPorts)
	index := sort.SearchInts(values, port)
	return index < len(values) && values[index] == port
}
func sorted(values []int) []int { copy := append([]int(nil), values...); sort.Ints(copy); return copy }
