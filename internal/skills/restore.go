package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RestoreOptions selects which sync records to undo.
type RestoreOptions struct {
	Names     []string
	Locations []string // only these location IDs; empty means all
	All       bool
	DryRun    bool
}

// Restore puts back what syncs replaced: the original directory (moved back
// from the backups, so every byte and timestamp is as it was), the original
// link, or nothing where there was nothing. An entry changed after the sync
// is left alone. The source directory is kept.
func (m *Manager) Restore(opts RestoreOptions) (*Result, error) {
	if len(opts.Names) == 0 && !opts.All {
		return nil, errors.New("请指定 Skill 名称，或用 --all 还原全部")
	}
	for _, id := range opts.Locations {
		if _, ok := m.location(id); !ok {
			return nil, fmt.Errorf("未知的位置 %q", id)
		}
	}
	if !opts.DryRun {
		unlock, err := m.lock()
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	st, err := m.load()
	if err != nil {
		return nil, err
	}
	names, locs := set(opts.Names), set(opts.Locations)
	res := &Result{DryRun: opts.DryRun}
	for _, rec := range append([]Record(nil), st.Records...) {
		if len(names) > 0 && !names[rec.Skill] || len(locs) > 0 && !locs[rec.Location] {
			continue
		}
		a := Action{Skill: rec.Skill, Location: rec.Location, Path: rec.Path, Do: "restore", Backup: rec.Backup}
		if ok, why := m.restorable(rec); !ok {
			a.Do, a.Reason = "conflict", why
		} else if !opts.DryRun {
			if err := m.restoreOne(st, rec); err != nil {
				a.Error = err.Error()
			} else {
				a.Done = true
			}
		}
		res.Actions = append(res.Actions, a)
	}
	for n := range names {
		if _, i := firstRecord(st, n); i < 0 && !hasAction(res, n) {
			res.Actions = append(res.Actions, Action{Skill: n, Do: "skip", Reason: "没有这个 Skill 的同步记录"})
		}
	}
	return res, nil
}

func set(list []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range list {
		out[s] = true
	}
	return out
}

func firstRecord(st *state, skill string) (Record, int) {
	for i, r := range st.Records {
		if r.Skill == skill {
			return r, i
		}
	}
	return Record{}, -1
}

func hasAction(r *Result, skill string) bool {
	for _, a := range r.Actions {
		if a.Skill == skill {
			return true
		}
	}
	return false
}

// restorable checks the entry is still what the sync wrote (or is gone) and
// that the original can be put back.
func (m *Manager) restorable(rec Record) (bool, string) {
	fi, err := os.Lstat(rec.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return false, err.Error()
	default:
		c := Copy{}
		if isLink(fi) {
			c.Kind = "link"
			c.Target, _ = readLink(rec.Path)
		} else if fi.IsDir() {
			c.Kind = "dir"
			if t, err := readTree(rec.Path); err == nil {
				c.Hash = t.Hash
			}
		}
		if !stillWritten(rec, c) {
			return false, "同步之后被改动过，没有还原" + backupNote(rec)
		}
	}
	if rec.Original.Kind == "dir" {
		if _, err := os.Stat(rec.Backup); err != nil {
			return false, "找不到备份 " + rec.Backup
		}
	}
	return true, ""
}

func backupNote(rec Record) string {
	if rec.Backup != "" {
		return "；原来的目录在 " + rec.Backup
	}
	if rec.Original.Kind == "link" {
		return "；原来是指向 " + rec.Original.Link + " 的链接"
	}
	return ""
}

func (m *Manager) restoreOne(st *state, rec Record) error {
	// Make the original link next to the entry first, so a link this system
	// cannot make (a symlink without the privilege) leaves everything as is.
	staged := ""
	if rec.Original.Kind == "link" {
		staged = rec.Path + ".mcpcell-restore"
		_ = os.Remove(staged)
		if err := relink(rec.Original, staged); err != nil {
			return fmt.Errorf("无法重建原来指向 %s 的链接：%w", rec.Original.Link, err)
		}
	}
	if fi, err := os.Lstat(rec.Path); err == nil {
		if isLink(fi) {
			err = os.Remove(rec.Path) // the link only, never its target
		} else {
			err = os.RemoveAll(rec.Path) // MCPcell's copy, checked unchanged
		}
		if err != nil {
			if staged != "" {
				_ = os.Remove(staged)
			}
			return err
		}
	}
	switch rec.Original.Kind {
	case "dir":
		if err := moveTree(rec.Backup, rec.Path); err != nil {
			return err
		}
		if t, err := readTree(rec.Path); err != nil || t.Hash != rec.Original.Hash {
			return errors.New("已移回原目录，但内容与同步前记录的不一致")
		}
		_ = os.Remove(filepath.Dir(rec.Backup)) // empty backup folders
		_ = os.Remove(filepath.Dir(filepath.Dir(rec.Backup)))
	case "link":
		if err := os.Rename(staged, rec.Path); err != nil {
			return fmt.Errorf("无法放回原来指向 %s 的链接：%w", rec.Original.Link, err)
		}
	}
	if rec.MadeDir {
		_ = os.Remove(filepath.Dir(rec.Path)) // only if empty
	}
	st.drop(rec.Skill, rec.Location)
	return m.save(st)
}
