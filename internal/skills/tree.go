package skills

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Tree is a skill directory's content: every file by slash-separated
// relative path. Two directories with the same Hash hold the same files.
type Tree struct {
	Files  map[string]string // path -> sha256 of the content, or "link:" + target
	Hash   string
	Newest time.Time // latest file modification
}

// ignored names never count as content: caches that running a skill's
// scripts or browsing it leaves behind. They are still copied and moved.
func ignored(name string, dir bool) bool {
	switch name {
	case ".git", "__pycache__":
		return dir
	case ".DS_Store", "Thumbs.db", "desktop.ini":
		return !dir
	}
	return !dir && strings.HasSuffix(name, ".pyc")
}

// readTree hashes the directory dir (following dir itself if it is a link,
// but no link inside it).
func readTree(dir string) (*Tree, error) {
	root, err := resolve(dir)
	if err != nil {
		return nil, err
	}
	t := &Tree{Files: map[string]string{}}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if ignored(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(t.Newest) {
			t.Newest = info.ModTime()
		}
		if !info.Mode().IsRegular() {
			target, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("%s 既不是文件也不是链接", rel)
			}
			t.Files[rel] = "link:" + filepath.ToSlash(target)
			return nil
		}
		sum, err := fileHash(p)
		if err != nil {
			return err
		}
		t.Files[rel] = sum
		return nil
	})
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(t.Files))
	for p := range t.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s\x00%s\n", p, t.Files[p])
	}
	t.Hash = hex.EncodeToString(h.Sum(nil))
	return t, nil
}

// statTree lists dir's files and their latest modification without reading
// them. Its Files have no hashes and its Hash is empty.
func statTree(dir string) (*Tree, error) {
	root, err := resolve(dir)
	if err != nil {
		return nil, err
	}
	t := &Tree{Files: map[string]string{}}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if ignored(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(t.Newest) {
			t.Newest = info.ModTime()
		}
		rel, _ := filepath.Rel(root, p)
		t.Files[filepath.ToSlash(rel)] = ""
		return nil
	})
	return t, err
}

func fileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// resolve follows p while it is a link (symlink or junction).
func resolve(p string) (string, error) {
	for range 16 {
		fi, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		if !isLink(fi) {
			return p, nil
		}
		target, err := readLink(p)
		if err != nil {
			return "", err
		}
		p = target
	}
	return "", errors.New("链接层级过多")
}

// readLink returns the absolute target of the link p.
func readLink(p string) (string, error) {
	target, err := os.Readlink(p)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(p), target)
	}
	return filepath.Clean(target), nil
}

// isLink: a symlink, or on Windows a junction.
func isLink(fi os.FileInfo) bool { return fi.Mode()&os.ModeSymlink != 0 || isJunction(fi) }

// Diff lists how a copy differs from the reference version.
type Diff struct {
	Added   []string `json:"added,omitempty"`   // only in this copy
	Removed []string `json:"removed,omitempty"` // only in the reference
	Changed []string `json:"changed,omitempty"`
}

func diffTrees(ref, t *Tree) *Diff {
	d := &Diff{}
	for p, h := range t.Files {
		switch rh, ok := ref.Files[p]; {
		case !ok:
			d.Added = append(d.Added, p)
		case rh != h:
			d.Changed = append(d.Changed, p)
		}
	}
	for p := range ref.Files {
		if _, ok := t.Files[p]; !ok {
			d.Removed = append(d.Removed, p)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.Changed)
	return d
}

// Summary is a one-line description of d, in Chinese.
func (d *Diff) Summary() string {
	if d == nil {
		return ""
	}
	var parts []string
	list := func(label string, ps []string) {
		if len(ps) == 0 {
			return
		}
		shown := ps
		if len(shown) > 3 {
			shown = shown[:3]
		}
		s := label + " " + strings.Join(shown, "、")
		if len(ps) > 3 {
			s += fmt.Sprintf(" 等 %d 个", len(ps))
		}
		parts = append(parts, s)
	}
	list("改动", d.Changed)
	list("多出", d.Added)
	list("缺少", d.Removed)
	return strings.Join(parts, "；")
}

// copyTree copies src (a directory, followed if it is a link) to dst, which
// must not exist. Files keep their modification times; links inside are
// copied as links.
func copyTree(src, dst string) error {
	root, err := resolve(src)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s 已存在", dst)
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(out, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			if err := copyFile(p, out, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chtimes(out, info.ModTime(), info.ModTime())
		default:
			target, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("无法复制 %s：%w", rel, err)
			}
			if isJunction(info) {
				return makeJunction(target, out)
			}
			return makeSymlink(target, out)
		}
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// moveTree moves the directory src to dst (which must not exist). A rename
// keeps every byte and timestamp. Across volumes it copies, checks the copy
// and only then removes src. Any other rename failure (a file in use) leaves
// src alone.
func moveTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !crossDevice(err) {
		return fmt.Errorf("无法移动 %s（可能有文件正被占用）：%w", src, err)
	}
	before, err := readTree(src)
	if err != nil {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	after, err := readTree(dst)
	if err != nil || after.Hash != before.Hash {
		_ = os.RemoveAll(dst)
		return fmt.Errorf("复制 %s 后校验不一致，没有移动", src)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("已复制到 %s，但没能删除原目录 %s：%w", dst, src, err)
	}
	return nil
}

// description reads the description from SKILL.md's front matter (one line,
// a folded or literal block, or a quoted string).
func description(dir string) string {
	f, err := os.Open(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	if !sc.Scan() || strings.TrimSpace(strings.TrimPrefix(sc.Text(), string(rune(0xFEFF)))) != "---" {
		return ""
	}
	var out []string
	in := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if in {
			if line != "" && line[0] != ' ' && line[0] != '\t' {
				break
			}
			if s := strings.TrimSpace(line); s != "" {
				out = append(out, s)
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			v = strings.TrimSpace(v)
			if v == "" || strings.HasPrefix(v, ">") || strings.HasPrefix(v, "|") {
				in = true
				continue
			}
			out = append(out, strings.Trim(v, `"'`))
			break
		}
	}
	s := strings.Join(out, " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}
