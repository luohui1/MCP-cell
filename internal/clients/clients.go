// Package clients reads AI clients' MCP configurations and points chosen
// entries at MCPcell ("takeover"), keeping exactly what is needed to put them
// back. Only the selected entry changes; every other byte of the file stays.
// Each change is recorded, and a full copy of the file is kept before the
// first change.
package clients

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry is one MCP server as a client configures it.
type Entry struct {
	Client  string            `json:"client"`
	Name    string            `json:"name"`
	Kind    string            `json:"kind"` // stdio | http
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"-"` // values never leave the daemon
	EnvKeys []string          `json:"envKeys,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Enabled bool              `json:"enabled"`
	// EnvVars are names a client forwards from its own environment (Codex
	// env_vars); MCPcell cannot see those values.
	EnvVars []string `json:"envVars,omitempty"`
}

func (e *Entry) setEnv(env map[string]string) {
	e.Env = env
	e.EnvKeys = e.EnvKeys[:0]
	for k := range env {
		e.EnvKeys = append(e.EnvKeys, k)
	}
	sort.Strings(e.EnvKeys)
}

// Record is one takeover: enough to verify and undo it.
type Record struct {
	Client   string    `json:"client"`
	Name     string    `json:"name"`
	Service  string    `json:"service"`
	Original string    `json:"original"` // the entry as it was (client-specific text)
	Written  string    `json:"written"`  // what MCPcell wrote, to detect later edits
	Backup   string    `json:"backup,omitempty"`
	At       time.Time `json:"at"`
	// Added marks an entry MCPcell created in a client that did not have it;
	// undoing it removes the entry.
	Added bool `json:"added,omitempty"`
}

// Client is one AI client's configuration.
type Client interface {
	ID() string
	Name() string
	Path() string
	List() ([]Entry, error)
	// Attach points entry name at url, authorized with token.
	Attach(name, url, token string) (Record, error)
	// Add creates entry name pointing at url in a client that lacks it.
	Add(name, url, token string) (Record, error)
	// Restore undoes Attach (puts the original back) or Add (removes the
	// entry). It refuses (ErrConflict) when the entry is no longer what
	// MCPcell wrote.
	Restore(rec Record) error
}

var (
	ErrConflict = errors.New("条目已被外部修改")
	ErrNotFound = errors.New("配置里没有这个条目")
	ErrExists   = errors.New("这个客户端里已经有同名条目")
)

// All returns the supported clients for this user. backups is where full
// copies of a config file go before its first change.
func All(home, backups string) []Client {
	return []Client{
		NewClaudeCode(home, backups, nil),
		NewCodex(home, backups),
		NewCursor(home, backups),
	}
}

// rewrite applies edit to path atomically and refuses if the file changed
// between reading and writing. It keeps one full backup per call.
func rewrite(path, backups, client string, edit func(old []byte) ([]byte, error)) (backup string, err error) {
	old, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	updated, err := edit(old)
	if err != nil {
		return "", err
	}
	if backup, err = keepBackup(backups, client, path, old); err != nil {
		return "", fmt.Errorf("无法备份 %s：%w", path, err)
	}
	if again, err := os.ReadFile(path); err != nil || !bytes.Equal(again, old) {
		return "", fmt.Errorf("%s 在修改期间被其他程序改动，没有写入，请重试", filepath.Base(path))
	}
	tmp := path + ".mcpcell.tmp"
	info, _ := os.Stat(path)
	mode := os.FileMode(0o600)
	if info != nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(tmp, updated, mode); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return backup, nil
}

const keepBackups = 20

func keepBackup(dir, client, path string, data []byte) (string, error) {
	if dir == "" {
		return "", nil
	}
	d := filepath.Join(dir, client)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	name := filepath.Join(d, time.Now().Format("20060102-150405.000")+"-"+filepath.Base(path))
	if err := os.WriteFile(name, data, 0o600); err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(d)
	for i := 0; i+keepBackups < len(entries); i++ {
		_ = os.Remove(filepath.Join(d, entries[i].Name()))
	}
	return name, nil
}

// newline returns the file's line ending.
func newline(b []byte) string {
	if bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}
