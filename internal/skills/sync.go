package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SyncOptions selects what Sync does.
type SyncOptions struct {
	Names []string
	// All syncs every skill found in more than one place that is not
	// unified yet (skills whose copies differ are only reported).
	All bool
	// To lists the location IDs to point at the source. Empty means the
	// locations that already have the skill. A location named here also
	// gets its link to another tool's directory replaced.
	To []string
	// From picks the version that wins: a location ID, or "mcpcell" for the
	// source. Empty: the source if it has the skill, otherwise the copies
	// must all be identical. Naming a version also replaces the copies that
	// differ from it (after backing them up).
	From string
	// Copy writes copies instead of links (links fall back to copies anyway
	// when the file system cannot make them).
	Copy   bool
	DryRun bool
}

// Action is one step of a sync or restore, planned or done.
type Action struct {
	Skill    string `json:"skill"`
	Location string `json:"location"` // a Location ID, or "mcpcell" for the source
	Path     string `json:"path,omitempty"`
	// Do: import / update-source (the source); link, replace, update (a
	// location); restore; ok (nothing to do); skip and conflict (left alone).
	Do     string `json:"do"`
	Copy   bool   `json:"copy,omitempty"` // a copy instead of a link
	From   string `json:"from,omitempty"` // import / update-source: the location copied
	Reason string `json:"reason,omitempty"`
	// Variant and Diff explain a conflict: this copy's version and how it
	// differs from the version that would win (or the newest).
	Variant string `json:"variant,omitempty"`
	Match   string `json:"match,omitempty"`
	Diff    *Diff  `json:"diff,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Error   string `json:"error,omitempty"`
	Backup  string `json:"backup,omitempty"`
}

// Result is what a sync or restore planned and, unless it was a dry run,
// did.
type Result struct {
	Actions []Action `json:"actions"`
	DryRun  bool     `json:"dryRun,omitempty"`
}

// Changes counts the actions that change something.
func (r *Result) Changes() int {
	n := 0
	for _, a := range r.Actions {
		switch a.Do {
		case "ok", "skip", "conflict":
		default:
			n++
		}
	}
	return n
}

// Sync makes the named skills' locations link to the source, importing the
// skill into the source first when needed.
func (m *Manager) Sync(opts SyncOptions) (*Result, error) {
	for _, id := range opts.To {
		if _, ok := m.location(id); !ok {
			return nil, fmt.Errorf("未知的位置 %q", id)
		}
	}
	if opts.From != "" && opts.From != SourceID {
		if _, ok := m.location(opts.From); !ok {
			return nil, fmt.Errorf("未知的位置 %q", opts.From)
		}
	}
	for _, n := range opts.Names {
		if err := checkName(n); err != nil {
			return nil, fmt.Errorf("%w：%q", err, n)
		}
	}
	if len(opts.Names) == 0 && !opts.All {
		return nil, errors.New("请指定 Skill 名称，或用 --all 同步全部")
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
	status, err := m.scan(st, opts.Names, true)
	if err != nil {
		return nil, err
	}
	res := &Result{DryRun: opts.DryRun}
	for _, sk := range status.Skills {
		if opts.All && len(opts.Names) == 0 && sk.State != "same" && sk.State != "differs" {
			continue
		}
		p := m.plan(st, sk, opts)
		if !opts.DryRun {
			m.apply(st, sk, &p)
		}
		res.Actions = append(res.Actions, p.acts...)
	}
	return res, nil
}

type plan struct {
	acts []Action
	win  *Copy // the version that wins
}

func copyAt(sk Skill, loc string) *Copy {
	for i := range sk.Copies {
		if sk.Copies[i].Location == loc {
			return &sk.Copies[i]
		}
	}
	return nil
}

func (m *Manager) plan(st *state, sk Skill, opts SyncOptions) plan {
	skip := func(loc, reason string) plan {
		return plan{acts: []Action{{Skill: sk.Name, Location: loc, Do: "skip", Reason: reason}}}
	}
	if len(sk.Copies) == 0 {
		return skip("", errNowhere.Error())
	}
	src := copyAt(sk, SourceID)
	var win *Copy
	switch opts.From {
	case SourceID:
		if src == nil {
			return skip(SourceID, "MCPcell 里还没有这个 Skill")
		}
		win = src
	case "":
		if src != nil {
			win = src
			break
		}
		var cands []*Copy
		hashes := map[string]bool{}
		for i := range sk.Copies {
			if c := &sk.Copies[i]; c.Kind == "dir" && c.tree != nil && !c.Changed {
				cands = append(cands, c)
				hashes[c.Hash] = true
			}
		}
		if len(cands) == 0 {
			if sk.State == "external" {
				return skip("", "各处都链接到 "+sk.Copies[0].Target+"，由其他工具管理；要改由 MCPcell 管理，用 --from <位置> 并在 --to 里列出要换的位置")
			}
			return skip("", "没有可以导入的副本（只有链接）")
		}
		if len(hashes) > 1 {
			var p plan
			for i := range sk.Copies {
				c := &sk.Copies[i]
				if c.tree == nil {
					continue
				}
				p.acts = append(p.acts, Action{Skill: sk.Name, Location: c.Location, Path: c.Path, Do: "conflict",
					Variant: c.Variant, Match: c.Match, Diff: c.Diff,
					Reason: "各处内容不同，用 --from <位置> 选择以哪一份为准"})
			}
			return p
		}
		win = cands[0]
	default:
		win = copyAt(sk, opts.From)
		if win == nil || win.tree == nil {
			return skip(opts.From, "这个位置没有这个 Skill")
		}
		if win.Changed {
			return skip(opts.From, "MCPcell 同步之后被改动过；先还原或手动处理")
		}
	}

	p := plan{win: win}
	srcPath := filepath.Join(m.SourceDir(), sk.Name)
	if win.Location != SourceID {
		switch {
		case src == nil:
			p.acts = append(p.acts, Action{Skill: sk.Name, Location: SourceID, Path: srcPath, Do: "import", From: win.Location})
		case src.Hash != win.Hash:
			p.acts = append(p.acts, Action{Skill: sk.Name, Location: SourceID, Path: srcPath, Do: "update-source", From: win.Location, Diff: diffTrees(src.tree, win.tree)})
		}
	}

	targets := opts.To
	explicit := map[string]bool{}
	for _, id := range opts.To {
		explicit[id] = true
	}
	if len(targets) == 0 {
		for _, c := range sk.Copies {
			if c.Location != SourceID {
				targets = append(targets, c.Location)
			}
		}
	}
	for _, id := range targets {
		l, _ := m.location(id)
		path := filepath.Join(l.Path, sk.Name)
		a := Action{Skill: sk.Name, Location: id, Path: path, Copy: opts.Copy}
		c := copyAt(sk, id)
		rec, ri := st.record(sk.Name, id)
		switch {
		case !l.Installed:
			a.Do, a.Reason = "skip", "没有找到 "+l.Name+" 的目录（"+filepath.Dir(l.Path)+"）"
		case c == nil:
			if _, err := os.Lstat(path); err == nil {
				a.Do, a.Reason = "skip", "已有同名的文件或目录，但不是 Skill"
			} else {
				a.Do = "link"
			}
		case c.Changed:
			a.Do, a.Reason = "skip", "MCPcell 同步之后被改动过，没有覆盖；可先 mcpcell skills restore"
		case c.Kind == "linked":
			a.Do = "ok"
		case ri >= 0 && rec.Wrote.Kind == "copy":
			a.Copy = true
			if c.Hash == win.Hash {
				a.Do = "ok"
			} else {
				a.Do = "update"
			}
		case c.Kind == "dir":
			if c.Hash == win.Hash || opts.From != "" {
				a.Do = "replace"
				if c.Hash != win.Hash {
					a.Variant, a.Match, a.Diff = c.Variant, c.Match, diffTrees(win.tree, c.tree)
				}
			} else {
				a.Do, a.Reason = "conflict", "内容与要同步的版本不同，没有覆盖；用 --from 选择以哪一份为准"
				a.Variant, a.Match, a.Diff = c.Variant, c.Match, diffTrees(win.tree, c.tree)
			}
		default: // a link to elsewhere, or a broken one
			if explicit[id] {
				a.Do = "replace"
			} else {
				a.Do, a.Reason = "skip", "链接到 "+c.Target+"（由其他工具管理）；要改用 MCPcell 的版本，用 --to 明确指定"
			}
		}
		p.acts = append(p.acts, a)
	}
	return p
}

// apply carries out a plan, recording every change as soon as it is made.
func (m *Manager) apply(st *state, sk Skill, p *plan) {
	stamp := time.Now().Format("20060102-150405.000")
	src := filepath.Join(m.SourceDir(), sk.Name)
	for i := range p.acts {
		a := &p.acts[i]
		var err error
		switch a.Do {
		case "import", "update-source":
			err = m.importSkill(st, sk.Name, p.win, a, stamp)
			if err != nil {
				a.Error = err.Error()
				for j := i + 1; j < len(p.acts); j++ {
					if p.acts[j].Do != "ok" && p.acts[j].Do != "skip" && p.acts[j].Do != "conflict" {
						p.acts[j].Error = "没有执行：来源没有准备好"
					}
				}
				return
			}
		case "link", "replace":
			err = m.place(st, sk, a, src, stamp)
		case "update":
			err = m.refreshCopy(st, sk.Name, a, src)
		default:
			continue
		}
		if err != nil {
			a.Error = err.Error()
			continue
		}
		a.Done = true
	}
}

// importSkill copies the winning version into the source directory, moving
// a previous version of it to the backups.
func (m *Manager) importSkill(st *state, name string, win *Copy, a *Action, stamp string) error {
	src := filepath.Join(m.SourceDir(), name)
	if err := os.MkdirAll(m.SourceDir(), 0o700); err != nil {
		return err
	}
	tmp := src + ".mcpcell-tmp"
	_ = os.RemoveAll(tmp)
	if err := copyTree(win.Path, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if t, err := readTree(tmp); err != nil || t.Hash != win.Hash {
		_ = os.RemoveAll(tmp)
		return errors.New("复制后校验不一致，可能在复制时被改动，请重试")
	}
	info := Source{From: win.Location, At: time.Now()}
	if a.Do == "update-source" {
		backup := filepath.Join(m.backupDir(), stamp, SourceID, name)
		if err := moveTree(src, backup); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		info.Replaced, a.Backup = backup, backup
	}
	if err := os.Rename(tmp, src); err != nil {
		return err
	}
	st.Sources[name] = info
	return m.save(st)
}

// place puts a link to the source (or a copy of it) at a.Path, moving what
// was there to the backups first.
func (m *Manager) place(st *state, sk Skill, a *Action, src, stamp string) error {
	l, _ := m.location(a.Location)
	rec := Record{Skill: sk.Name, Location: a.Location, Path: a.Path, Original: Entry{Kind: "absent"}, At: time.Now()}
	if old, i := st.record(sk.Name, a.Location); i >= 0 && a.Do == "link" {
		// MCPcell's link was removed since an earlier sync. What was there
		// before that sync is still what a restore must put back.
		rec.Original, rec.Backup, rec.MadeDir = old.Original, old.Backup, old.MadeDir
	}
	if !isDir(l.Path) {
		if err := os.MkdirAll(l.Path, 0o755); err != nil {
			return err
		}
		rec.MadeDir = true
	}
	// Move the current entry out of the way, remembering how to undo it.
	undo := func() {}
	if a.Do == "replace" {
		fi, err := os.Lstat(a.Path)
		if err != nil {
			return err
		}
		if isLink(fi) {
			target, err := readLink(a.Path)
			if err != nil {
				return err
			}
			rec.Original = Entry{Kind: "link", Link: target, Junction: isJunction(fi)}
			if err := os.Remove(a.Path); err != nil {
				return err
			}
			undo = func() { _ = relink(rec.Original, a.Path) }
		} else {
			t, err := readTree(a.Path)
			if err != nil {
				return err
			}
			backup := filepath.Join(m.backupDir(), stamp, a.Location, sk.Name)
			if err := moveTree(a.Path, backup); err != nil {
				return err
			}
			rec.Original = Entry{Kind: "dir", Hash: t.Hash}
			rec.Backup, a.Backup = backup, backup
			undo = func() { _ = moveTree(backup, a.Path) }
		}
	}
	wrote, err := m.write(src, a.Path, a.Copy)
	if err != nil {
		undo()
		if rec.MadeDir {
			_ = os.Remove(l.Path)
		}
		return err
	}
	if wrote.Kind == "copy" && !a.Copy {
		a.Copy = true
		a.Reason = "无法创建目录链接，改为复制"
	}
	rec.Wrote = wrote
	st.put(rec)
	return m.save(st)
}

// write links path to src, or copies src there when asked to or when
// linking fails.
func (m *Manager) write(src, path string, copyOnly bool) (Entry, error) {
	if !copyOnly {
		junction, err := makeLink(src, path)
		if err == nil {
			target, err := readLink(path)
			if err != nil {
				target = src
			}
			return Entry{Kind: "link", Link: target, Junction: junction}, nil
		}
		_ = os.Remove(path)
	}
	t, err := readTree(src)
	if err != nil {
		return Entry{}, err
	}
	if err := copyTree(src, path); err != nil {
		_ = os.RemoveAll(path)
		return Entry{}, err
	}
	return Entry{Kind: "copy", Hash: t.Hash}, nil
}

// refreshCopy replaces MCPcell's own copy with the source's current content.
func (m *Manager) refreshCopy(st *state, name string, a *Action, src string) error {
	rec, i := st.record(name, a.Location)
	if i < 0 {
		return errors.New("没有同步记录")
	}
	tmp := a.Path + ".mcpcell-tmp"
	_ = os.RemoveAll(tmp)
	t, err := readTree(src)
	if err != nil {
		return err
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(a.Path); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, a.Path); err != nil {
		return err
	}
	rec.Wrote = Entry{Kind: "copy", Hash: t.Hash}
	rec.At = time.Now()
	st.put(rec)
	return m.save(st)
}

// relink recreates a link found before a sync.
func relink(e Entry, path string) error {
	if e.Junction {
		return makeJunction(e.Link, path)
	}
	return makeSymlink(e.Link, path)
}
