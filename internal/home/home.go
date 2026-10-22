// Package home finds MCPcell's data directory and the running daemon. It is
// kept apart from registry so that small programs (the tray) can read them
// without linking crypto/rand, which costs every Go process on Windows a
// fixed 32 MB of committed memory (docs/architecture.md §13).
package home

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir is $MCPCELL_HOME or ~/.mcpcell.
func Dir() (string, error) {
	if h := os.Getenv("MCPCELL_HOME"); h != "" {
		return filepath.Abs(h)
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".mcpcell"), nil
}

// Daemon is written by a running daemon so other programs can find it.
type Daemon struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
}

func DaemonPath(dir string) string { return filepath.Join(dir, "daemon.json") }

// ReadDaemon reads daemon.json; an error means no daemon is known to run.
func ReadDaemon(dir string) (Daemon, error) {
	var d Daemon
	b, err := os.ReadFile(DaemonPath(dir))
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal(b, &d)
}

// ReadToken returns the daemon's bearer token without creating one.
func ReadToken(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "token"))
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(b))
	if len(t) < 32 {
		return "", errors.New("token 文件无效")
	}
	return t, nil
}
