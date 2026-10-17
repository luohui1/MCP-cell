// Package registry loads service definitions and the daemon's local state
// (token, port, pid) from the MCPcell home directory.
package registry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	DefaultPort        = 47621
	DefaultIdleTimeout = 10 * time.Minute
	ModeShared         = "shared"
)

// Config is config.json. Service entries use the same shape as a client's
// mcpServers entry, so existing definitions can be pasted in.
type Config struct {
	Port     int                 `json:"port,omitempty"`
	Services map[string]*Service `json:"services"`
}

type Service struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	Mode        string            `json:"mode,omitempty"`
	IdleTimeout Duration          `json:"idleTimeout,omitzero"`
	Disabled    bool              `json:"disabled,omitempty"`
}

// Duration reads "10m", "90s" or "0" (never reap). Set distinguishes an
// explicit "0" from an omitted field, which gets the default.
type Duration struct {
	time.Duration
	Set bool
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("时长应写成字符串，例如 \"10m\"")
	}
	v := time.Duration(0)
	if s != "0" {
		var err error
		if v, err = time.ParseDuration(s); err != nil || v < 0 {
			return fmt.Errorf("无效的时长 %q", s)
		}
	}
	d.Duration, d.Set = v, true
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Home is $MCPCELL_HOME or ~/.mcpcell.
func Home() (string, error) {
	if h := os.Getenv("MCPCELL_HOME"); h != "" {
		return filepath.Abs(h)
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".mcpcell"), nil
}

func ConfigPath(home string) string { return filepath.Join(home, "config.json") }

// Load reads config.json, creating an empty one on first run.
func Load(home string) (*Config, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	path := ConfigPath(home)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := &Config{Port: DefaultPort, Services: map[string]*Service{}}
		out, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Config, error) {
	var cfg Config
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config.json 格式错误：%w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Services == nil {
		cfg.Services = map[string]*Service{}
	}
	for name, s := range cfg.Services {
		if err := s.normalize(name); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

func (s *Service) normalize(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("服务名 %q 只能包含字母、数字、- 和 _", name)
	}
	if s == nil || strings.TrimSpace(s.Command) == "" {
		return fmt.Errorf("服务 %s 缺少 command", name)
	}
	switch s.Mode {
	case "":
		s.Mode = ModeShared
	case ModeShared:
	case "per-session", "per-workspace":
		return fmt.Errorf("服务 %s：%s 模式在 M3 实现，当前只支持 shared", name, s.Mode)
	default:
		return fmt.Errorf("服务 %s：未知模式 %q", name, s.Mode)
	}
	if !s.IdleTimeout.Set {
		s.IdleTimeout = Duration{DefaultIdleTimeout, true}
	}
	return nil
}

// Names returns enabled service names in stable order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Services))
	for n, s := range c.Services {
		if !s.Disabled {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Token returns the daemon's bearer token, creating it on first use.
func Token(home string) (string, error) {
	path := filepath.Join(home, "token")
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) >= 32 {
			return t, nil
		}
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	t := hex.EncodeToString(buf)
	return t, os.WriteFile(path, []byte(t+"\n"), 0o600)
}

// DaemonInfo is written by a running daemon so the CLI can find it.
type DaemonInfo struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
}

func daemonPath(home string) string { return filepath.Join(home, "daemon.json") }

func WriteDaemonInfo(home string, info DaemonInfo) error {
	b, _ := json.MarshalIndent(info, "", "  ")
	return os.WriteFile(daemonPath(home), b, 0o600)
}

func ReadDaemonInfo(home string) (DaemonInfo, error) {
	var info DaemonInfo
	b, err := os.ReadFile(daemonPath(home))
	if err != nil {
		return info, err
	}
	return info, json.Unmarshal(b, &info)
}

func RemoveDaemonInfo(home string) { _ = os.Remove(daemonPath(home)) }
