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
	DefaultPort         = 47621
	DefaultIdleTimeout  = 3 * time.Minute
	DefaultMaxInstances = 32
	// A per-session instance holds the session's state (browser pages,
	// connections); reaping it loses that, so it waits longer.
	DefaultSessionIdle = 30 * time.Minute

	// ModeShared runs one instance for every session.
	ModeShared = "shared"
	// ModePerSession gives each client session its own instance, started on
	// its first call and stopped when the session ends.
	ModePerSession = "per-session"
)

// Config is config.json. Service entries use the same shape as a client's
// mcpServers entry, so existing definitions can be pasted in.
type Config struct {
	Port     int                 `json:"port,omitempty"`
	Limits   Limits              `json:"limits,omitzero"`
	Services map[string]*Service `json:"services"`
}

type Limits struct {
	// MaxInstances caps running upstream instances across all services. When
	// full, the least recently used idle instance is stopped to make room.
	MaxInstances int `json:"maxInstances,omitempty"`
}

type Service struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	Mode        string            `json:"mode,omitempty"`
	IdleTimeout Duration          `json:"idleTimeout,omitzero"`
	// MemoryLimit caps the committed memory of the service's whole process
	// tree, e.g. "1GB" (Windows only for now). Allocations beyond it fail.
	MemoryLimit Size `json:"memoryLimit,omitzero"`
	// Flatten runs `npx pkg` as `node <bin>` from MCPcell's package cache.
	// Default true; set false to keep npx.
	Flatten *bool `json:"flatten,omitempty"`
	// Lean starts Node.js servers with memory-saving V8 flags. Default true.
	Lean *bool `json:"lean,omitempty"`
	// Catalog "off" disables caching the server's tool/prompt/resource lists,
	// for servers whose lists change at runtime without notifying.
	Catalog  string `json:"catalog,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

func (s *Service) FlattenEnabled() bool { return s.Flatten == nil || *s.Flatten }
func (s *Service) LeanEnabled() bool    { return s.Lean == nil || *s.Lean }

// Idle is the effective idle timeout: the configured one, or the mode's
// default. 0 means never reap.
func (s *Service) Idle() time.Duration {
	switch {
	case s.IdleTimeout.Set:
		return s.IdleTimeout.Duration
	case s.PerSession():
		return DefaultSessionIdle
	}
	return DefaultIdleTimeout
}

func (s *Service) PerSession() bool     { return s.Mode == ModePerSession }
func (s *Service) CatalogEnabled() bool { return s.Catalog != "off" }

// Size reads "512MB", "1.5GB", "800KB" or "0".
type Size uint64

func (z *Size) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("大小应写成字符串，例如 \"1GB\"")
	}
	u := strings.ToUpper(strings.TrimSpace(s))
	mult := 1.0
	for _, suf := range []struct {
		s string
		m float64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(u, suf.s) {
			u, mult = strings.TrimSpace(strings.TrimSuffix(u, suf.s)), suf.m
			break
		}
	}
	var v float64
	if _, err := fmt.Sscanf(u, "%g", &v); err != nil || v < 0 {
		return fmt.Errorf("无效的大小 %q", s)
	}
	*z = Size(v * mult)
	return nil
}

func (z Size) MarshalJSON() ([]byte, error) { return json.Marshal(z.Text()) }

func (z Size) Text() string {
	if z > 0 && z%(1<<30) == 0 {
		return fmt.Sprintf("%dGB", uint64(z)>>30)
	}
	return fmt.Sprintf("%dMB", uint64(z)>>20)
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

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.Text()) }

// Text formats the duration the way people write it in config.json.
func (d Duration) Text() string {
	switch v := d.Duration; {
	case v == 0:
		return "0"
	case v%time.Hour == 0:
		return fmt.Sprintf("%dh", v/time.Hour)
	case v%time.Minute == 0:
		return fmt.Sprintf("%dm", v/time.Minute)
	case v%time.Second == 0:
		return fmt.Sprintf("%ds", v/time.Second)
	}
	return d.Duration.String()
}

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
	if cfg.Limits.MaxInstances <= 0 {
		cfg.Limits.MaxInstances = DefaultMaxInstances
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
	case ModeShared, ModePerSession:
	case "per-workspace":
		return fmt.Errorf("服务 %s：per-workspace 模式尚未实现，可以用 per-session", name)
	default:
		return fmt.Errorf("服务 %s：未知模式 %q", name, s.Mode)
	}
	if s.Catalog != "" && s.Catalog != "off" {
		return fmt.Errorf("服务 %s：catalog 只能是 \"off\" 或不写", name)
	}
	if s.MemoryLimit > 0 && s.MemoryLimit < 32<<20 {
		return fmt.Errorf("服务 %s：memoryLimit 太小（至少 32MB）", name)
	}
	return nil
}

// ParseService validates one service entry as it would appear in config.json.
func ParseService(name string, raw []byte) (*Service, error) {
	var s Service
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("服务 %s 的配置无效：%w", name, err)
	}
	if err := s.normalize(name); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes config.json atomically.
func Save(home string, cfg *Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := ConfigPath(home) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigPath(home))
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
