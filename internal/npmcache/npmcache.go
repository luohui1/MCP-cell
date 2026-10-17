// Package npmcache runs npx-launched MCP servers without npx. The package is
// installed once into MCPcell's own cache with its version pinned, then started
// as `node <bin script>`. That drops npx's own node process (~130 MB private
// commit on Windows, plus a cmd.exe) and the registry check npx does on every
// start, which is what makes npx servers miss clients' connect timeouts.
package npmcache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/luohui1/MCP-cell/internal/proc"
)

// Pin records which exact version a spec from the config resolved to.
type Pin struct {
	Spec        string    `json:"spec"` // as written in the config, e.g. @upstash/context7-mcp@latest
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Dir         string    `json:"dir"` // npm --prefix of this install
	Bin         string    `json:"bin"` // script path relative to the package directory
	InstalledAt time.Time `json:"installedAt"`
}

// Script is the absolute path of the package's bin script.
func (p *Pin) Script() string {
	return filepath.Join(p.Dir, "node_modules", filepath.FromSlash(p.Name), filepath.FromSlash(p.Bin))
}

// Installer runs `npm install` of spec into prefix. Replaced in tests.
type Installer func(ctx context.Context, spec, prefix string, env []string) error

type Cache struct {
	dir     string
	install Installer

	mu    sync.Mutex
	pins  map[string]*Pin
	locks map[string]*sync.Mutex
}

const installTimeout = 5 * time.Minute

// Open loads the pins in dir (~/.mcpcell/pkgs).
func Open(dir string, install Installer) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if install == nil {
		install = npmInstall
	}
	c := &Cache{dir: dir, install: install, pins: map[string]*Pin{}, locks: map[string]*sync.Mutex{}}
	if b, err := os.ReadFile(c.pinsPath()); err == nil {
		_ = json.Unmarshal(b, &c.pins)
	}
	return c, nil
}

func (c *Cache) pinsPath() string { return filepath.Join(c.dir, "pins.json") }

// Pinned returns the current pin for spec without installing anything.
func (c *Cache) Pinned(spec string) *Pin {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p := c.pins[spec]; p != nil {
		cp := *p
		return &cp
	}
	return nil
}

// Ensure returns spec's pin, installing the package the first time.
func (c *Cache) Ensure(ctx context.Context, spec string, env []string) (*Pin, error) {
	unlock := c.lock(spec)
	defer unlock()
	if p := c.Pinned(spec); p != nil {
		if _, err := os.Stat(p.Script()); err == nil {
			return p, nil
		}
	}
	return c.installPin(ctx, spec, env)
}

// Update reinstalls spec at the newest version its range allows and re-pins.
// It returns the previous pin (nil if there was none).
func (c *Cache) Update(ctx context.Context, spec string, env []string) (now, before *Pin, err error) {
	unlock := c.lock(spec)
	defer unlock()
	before = c.Pinned(spec)
	now, err = c.installPin(ctx, spec, env)
	if err == nil && before != nil && before.Dir != now.Dir && !c.referenced(before.Dir) {
		_ = os.RemoveAll(before.Dir) // best effort: a running server may still hold files
	}
	return now, before, err
}

func (c *Cache) lock(spec string) func() {
	c.mu.Lock()
	l := c.locks[spec]
	if l == nil {
		l = &sync.Mutex{}
		c.locks[spec] = l
	}
	c.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (c *Cache) referenced(dir string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.pins {
		if p.Dir == dir {
			return true
		}
	}
	return false
}

// installPin installs into a fresh temporary prefix, reads the version npm
// actually chose, and moves the install to <name>@<version>.
func (c *Cache) installPin(ctx context.Context, spec string, env []string) (*Pin, error) {
	name, _ := SplitSpec(spec)
	tmp := filepath.Join(c.dir, ".tmp-"+randomHex(6))
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	if err := c.install(ctx, spec, tmp, env); err != nil {
		return nil, err
	}
	version, bin, err := readPackage(filepath.Join(tmp, "node_modules", filepath.FromSlash(name)), name)
	if err != nil {
		return nil, err
	}
	final := filepath.Join(c.dir, strings.ReplaceAll(name, "/", "+")+"@"+version)
	if _, err := os.Stat(final); err != nil {
		if err := os.Rename(tmp, final); err != nil {
			return nil, fmt.Errorf("无法写入包缓存：%w", err)
		}
	}
	pin := &Pin{Spec: spec, Name: name, Version: version, Dir: final, Bin: bin, InstalledAt: time.Now()}
	c.mu.Lock()
	c.pins[spec] = pin
	err = c.savePinsLocked()
	c.mu.Unlock()
	cp := *pin
	return &cp, err
}

func (c *Cache) savePinsLocked() error {
	b, _ := json.MarshalIndent(c.pins, "", "  ")
	tmp := c.pinsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.pinsPath())
}

// readPackage returns the installed version and the bin script npx would run:
// the only bin, or the one named after the unscoped package name.
func readPackage(dir, name string) (version, bin string, err error) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "", "", fmt.Errorf("安装后找不到 %s 的 package.json", name)
	}
	var pkg struct {
		Version string          `json:"version"`
		Bin     json.RawMessage `json:"bin"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil || pkg.Version == "" {
		return "", "", fmt.Errorf("%s 的 package.json 无效", name)
	}
	var one string
	var many map[string]string
	switch {
	case json.Unmarshal(pkg.Bin, &one) == nil && one != "":
		bin = one
	case json.Unmarshal(pkg.Bin, &many) == nil && len(many) > 0:
		unscoped := name[strings.LastIndex(name, "/")+1:]
		if b, ok := many[unscoped]; ok {
			bin = b
		} else if len(many) == 1 {
			for _, b := range many {
				bin = b
			}
		}
	}
	if bin == "" {
		return "", "", fmt.Errorf("%s 没有可以直接运行的 bin 入口", name)
	}
	return pkg.Version, filepath.ToSlash(filepath.Clean(bin)), nil
}

func npmInstall(ctx context.Context, spec, prefix string, env []string) error {
	out, err := proc.Run(ctx, []string{"npm", "install", "--prefix", prefix, "--no-audit", "--no-fund",
		"--omit=dev", "--loglevel=error", spec}, env, prefix)
	if err != nil {
		tail := strings.TrimSpace(string(out))
		if lines := strings.Split(tail, "\n"); len(lines) > 6 {
			tail = strings.Join(lines[len(lines)-6:], "\n")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("npm install %s 超时", spec)
		}
		return fmt.Errorf("npm install %s 失败：%v\n%s", spec, err, tail)
	}
	return nil
}

var npmName = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9~][a-z0-9._~-]*$`)

// SplitSpec splits "@scope/name@range" into name and range ("" when absent).
func SplitSpec(spec string) (name, version string) {
	at := strings.LastIndex(spec, "@")
	if at <= 0 {
		return spec, ""
	}
	return spec[:at], spec[at+1:]
}

// Detect reports whether argv (already unwrapped from `cmd /c`) runs a
// registry package through npx in a way MCPcell can reproduce, and returns
// the package spec and the server's own arguments. Anything unusual (-p,
// --call, local paths, git URLs) is left to npx.
func Detect(argv []string) (spec string, args []string, ok bool) {
	if len(argv) < 2 {
		return "", nil, false
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	for _, ext := range []string{".cmd", ".exe", ".ps1"} {
		base = strings.TrimSuffix(base, ext)
	}
	if base != "npx" {
		return "", nil, false
	}
	i := 1
	for ; i < len(argv); i++ {
		a := argv[i]
		if a == "-y" || a == "--yes" || a == "-q" || a == "--quiet" || a == "--silent" {
			continue
		}
		if strings.HasPrefix(a, "-") {
			return "", nil, false
		}
		break
	}
	if i >= len(argv) {
		return "", nil, false
	}
	spec = argv[i]
	name, version := SplitSpec(spec)
	if !npmName.MatchString(name) || strings.ContainsAny(version, ` :/\`) {
		return "", nil, false
	}
	return spec, argv[i+1:], true
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
