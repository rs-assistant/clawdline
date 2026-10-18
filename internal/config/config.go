// Package config resolves where this daemon keeps its state and which ports it
// uses. Everything here is deliberately distinct from the retired Swift
// Clawdline's own identity — a different config directory, a different port, a
// different token file — so that installing this one never wrote over that
// one's state, and so that a machine which still has both installed keeps them
// apart.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	// DefaultPort is this daemon's own port.
	DefaultPort = 7727
	// AppDir is the per-user directory name. Not "clawdline": sharing it would
	// mean two writers over one orchestrator store.
	AppDir = "clawdline-next"
)

type Config struct {
	Port    int
	Dir     string
	WebRoot string
	// Host is what the server binds to. Loopback by default, because a daemon
	// that owns terminals and credentials should not become reachable by
	// accident. Binding wider is a decision somebody makes out loud.
	Host string
}

// Load resolves the configuration from the environment, falling back to the
// platform's ordinary location.
func Load() Config {
	host := "127.0.0.1"
	if v := os.Getenv("CLAWDLINE_NEXT_HOST"); v != "" {
		host = v
	}
	return Config{
		Port: DefaultPort,
		Dir:  Dir(),
		Host: host,
	}
}

// Dir is the durable state root. Task exchange, receipts and the event store
// live under it — never under a temporary directory, which the capability
// matrix recorded as a Linux blocker for the Swift app.
func Dir() string {
	if v := os.Getenv("CLAWDLINE_NEXT_DIR"); v != "" {
		return v
	}
	return DefaultDir()
}

// DefaultDir is the installed daemon's ordinary state location, independent
// of an environment override carried by a shell.
func DefaultDir() string {
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, AppDir)
		}
	case "darwin", "linux":
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, AppDir)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return AppDir
	}
	return filepath.Join(home, ".config", AppDir)
}
