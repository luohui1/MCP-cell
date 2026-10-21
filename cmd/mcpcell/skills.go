package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/luohui1/MCP-cell/internal/registry"
	"github.com/luohui1/MCP-cell/internal/skills"
)

// mcpcell skills works on files directly, so it needs no daemon. The daemon's
// Skill page uses the same package; a lock file keeps the two apart.

const skillsUsage = `用法:
  mcpcell skills [名称...]       列出每个 Skill 在哪些位置、各处是否一致；给出名称时显示每处的差异
  mcpcell skills dirs            列出各 Agent 读取的 Skill 目录
  mcpcell skills sync <名称...>|--all [--to 位置,...] [--from 位置|mcpcell] [--copy] [--dry-run] [-y]
                                 把 Skill 放进 MCPcell 的来源目录，并让各位置链接到它；
                                 --all 是所有在多处出现、还没统一的（内容不同的只列出差异）
  mcpcell skills restore <名称...>|--all [--to 位置,...] [-y]
                                 还原同步时替换的内容（逐字节），来源目录保留
`

// tilde shortens a path under the user's home directory.
func tilde(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && len(p) > len(h) && strings.EqualFold(p[:len(h)], h) && os.IsPathSeparator(p[len(h)]) {
		return "~" + p[len(h):]
	}
	return p
}

func openSkills() (*skills.Manager, error) {
	home, err := registry.Home()
	if err != nil {
		return nil, err
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return skills.New(home, user), nil
}

func runSkills(args []string) error {
	m, err := openSkills()
	if err != nil {
		return err
	}
	return skillsWith(os.Stdout, os.Stdin, m, args)
}

func skillsWith(w io.Writer, in io.Reader, m *skills.Manager, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "sync":
			return skillsSync(w, in, m, args[1:])
		case "restore":
			return skillsRestore(w, in, m, args[1:])
		case "dirs":
			skillDirs(w, m)
			return nil
		case "help", "-h", "--help":
			fmt.Fprint(w, skillsUsage)
			return nil
		}
		if strings.HasPrefix(args[0], "-") {
			return fmt.Errorf("未知参数 %s\n\n%s", args[0], skillsUsage)
		}
	}
	st, err := m.Scan(args...)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		for i, sk := range st.Skills {
			if i > 0 {
				fmt.Fprintln(w)
			}
			skillDetail(w, st, sk)
		}
		return nil
	}
	skillTable(w, st)
	return nil
}

var stateText = map[string]string{
	"synced": "已统一", "same": "相同", "differs": "不一致", "single": "单处",
	"external": "外部管理", "source": "仅来源",
}

// cellText is how one location's copy shows in the overview.
func cellText(sk skills.Skill, c *skills.Copy) string {
	if c == nil {
		return "-"
	}
	var s string
	switch {
	case c.Error != "":
		s = "读取失败"
	case c.Changed:
		s = "已改动"
	case c.Kind == "linked":
		s = "链接"
	case c.Kind == "broken":
		s = "失效链接"
	case c.Kind == "link":
		s = "外链"
	case c.Managed:
		s = "副本"
	case c.Location == skills.SourceID:
		return "有"
	default:
		s = map[string]string{"same": "一致", "newer": "较新", "older": "较旧", "differs": "不同"}[c.Match]
		if sk.Variants > 1 && c.Match == "same" && sk.Reference != skills.SourceID {
			s = "最新" // the reference is the newest copy
		}
	}
	if c.Variant != "" && c.Kind != "linked" {
		s = c.Variant + " " + s
	}
	return s
}

func findCopy(sk skills.Skill, loc string) *skills.Copy {
	for i := range sk.Copies {
		if sk.Copies[i].Location == loc {
			return &sk.Copies[i]
		}
	}
	return nil
}

func skillTable(w io.Writer, st *skills.Status) {
	var locs []skills.Location
	for _, l := range st.Locations {
		if l.Exists {
			locs = append(locs, l)
		}
	}
	fmt.Fprintf(w, "来源目录：%s\n\n", tilde(st.Source))
	if len(st.Skills) == 0 {
		fmt.Fprintln(w, "没有找到 Skill。")
		return
	}
	head := []string{"Skill", "状态", "来源"}
	for _, l := range locs {
		head = append(head, l.ID)
	}
	rows := [][]string{head}
	count := map[string]int{}
	for _, sk := range st.Skills {
		count[sk.State]++
		r := []string{sk.Name, stateText[sk.State], cellText(sk, findCopy(sk, skills.SourceID))}
		for _, l := range locs {
			r = append(r, cellText(sk, findCopy(sk, l.ID)))
		}
		rows = append(rows, r)
	}
	fprintTable(w, rows)
	fmt.Fprintf(w, "\n%d 个 Skill：已统一 %d，各处相同 %d，不一致 %d，只在一处 %d，由其他工具链接管理 %d",
		len(st.Skills), count["synced"], count["same"], count["differs"], count["single"], count["external"])
	if count["source"] > 0 {
		fmt.Fprintf(w, "，只在来源目录 %d", count["source"])
	}
	fmt.Fprintln(w, "。")
	fmt.Fprintln(w, "「链接」指向 MCPcell 来源目录；「外链」指向其他目录（如其他安装工具的目录）；A/B 是不同的版本。")
	fmt.Fprintln(w, "查看差异：mcpcell skills <名称>；统一：mcpcell skills sync <名称>；各位置对应的 Agent：mcpcell skills dirs")
}

func skillDirs(w io.Writer, m *skills.Manager) {
	fmt.Fprintf(w, "来源目录：%s\n\n", tilde(m.SourceDir()))
	rows := [][]string{{"位置", "目录", "读取它的 Agent", "说明"}}
	for _, l := range m.Dirs() {
		path := tilde(l.Path)
		switch {
		case !l.Installed:
			path += "（未安装）"
		case !l.Exists:
			path += "（还没有）"
		}
		rows = append(rows, []string{l.ID, path, strings.Join(l.Agents, "、"), l.Note})
	}
	fprintTable(w, rows)
}

func skillDetail(w io.Writer, st *skills.Status, sk skills.Skill) {
	if len(sk.Copies) == 0 {
		fmt.Fprintf(w, "%s：没有找到这个 Skill\n", sk.Name)
		return
	}
	fmt.Fprintln(w, sk.Name)
	if sk.Description != "" {
		fmt.Fprintln(w, "  "+sk.Description)
	}
	state := stateText[sk.State]
	if sk.Variants > 1 {
		state += fmt.Sprintf("（%d 个版本）", sk.Variants)
	}
	fmt.Fprintf(w, "  状态：%s", state)
	// The reference matters only when there is something to compare with.
	compared := sk.Variants > 1 || sk.Reference == skills.SourceID
	if compared {
		ref := "MCPcell 来源目录"
		if sk.Reference != skills.SourceID {
			ref = sk.Reference + "（最近修改的一份）"
		}
		fmt.Fprintf(w, " · 比较基准：%s", ref)
	}
	if sk.From != "" {
		fmt.Fprintf(w, " · 来源取自 %s", sk.From)
	}
	fmt.Fprintln(w)
	rows := [][]string{{"  位置", "内容", "文件", "最近修改", "路径"}}
	var diffs []string
	for i := range sk.Copies {
		c := &sk.Copies[i]
		what := cellText(sk, c)
		if compared && c.Location == sk.Reference && c.Location != skills.SourceID {
			what += "（基准）"
		}
		if c.Kind == "link" || c.Kind == "broken" {
			what += " → " + tilde(c.Target)
		}
		when := "-"
		if !c.Newest.IsZero() {
			when = c.Newest.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{"  " + c.Location, what, fmt.Sprint(c.Files), when, tilde(c.Path)})
		if s := c.Diff.Summary(); s != "" {
			diffs = append(diffs, fmt.Sprintf("  %s 与基准相比：%s", c.Location, s))
		}
		if c.Error != "" {
			diffs = append(diffs, fmt.Sprintf("  %s：%s", c.Location, c.Error))
		}
	}
	fprintTable(w, rows)
	for _, d := range diffs {
		fmt.Fprintln(w, d)
	}
	switch sk.State {
	case "differs":
		fmt.Fprintf(w, "  选定一份为准后统一：mcpcell skills sync %s --from <位置>（其他不同的副本会先备份）\n", sk.Name)
	case "same", "single":
		fmt.Fprintf(w, "  统一：mcpcell skills sync %s\n", sk.Name)
	case "external":
		fmt.Fprintln(w, "  各处都链接到同一个外部目录，已经是一份；由安装它的工具更新。")
	}
}

type skillFlags struct {
	names        []string
	all, yes     bool
	dryRun, copy bool
	to           listFlag
	from         string
}

func parseSkillFlags(cmd string, args []string) (o skillFlags, err error) {
	fs := flag.NewFlagSet("skills "+cmd, flag.ContinueOnError)
	fs.BoolVar(&o.all, "all", false, "sync：所有在多处出现、还没统一的 Skill；restore：全部同步记录")
	fs.BoolVar(&o.yes, "y", false, "不询问，直接执行")
	fs.BoolVar(&o.yes, "yes", false, "同 -y")
	fs.Var(&o.to, "to", "只处理这些位置（逗号分隔，见 mcpcell skills dirs）")
	if cmd == "sync" {
		fs.StringVar(&o.from, "from", "", "以这个位置的版本为准（mcpcell 表示来源目录）")
		fs.BoolVar(&o.copy, "copy", false, "复制而不是链接")
		fs.BoolVar(&o.dryRun, "dry-run", false, "只列出计划")
	}
	fs.Usage = func() { fmt.Fprint(fs.Output(), skillsUsage) }
	for {
		if err := fs.Parse(args); err != nil {
			return o, err
		}
		if fs.NArg() == 0 {
			break
		}
		o.names = append(o.names, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(o.names) == 0 && !o.all {
		return o, fmt.Errorf("请指定 Skill 名称，或用 --all\n\n%s", skillsUsage)
	}
	return o, nil
}

func skillsSync(w io.Writer, in io.Reader, m *skills.Manager, args []string) error {
	o, err := parseSkillFlags("sync", args)
	if err != nil {
		return err
	}
	opts := skills.SyncOptions{Names: o.names, All: o.all, To: o.to, From: o.from, Copy: o.copy, DryRun: true}
	plan, err := m.Sync(opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "来源目录：%s\n\n", tilde(m.SourceDir()))
	printActions(w, plan.Actions, true)
	n := plan.Changes()
	if n == 0 {
		fmt.Fprintln(w, "\n没有需要改动的。")
		if hasDo(plan.Actions, "conflict") {
			return errors.New("有内容不同的副本，没有覆盖")
		}
		return nil
	}
	if o.dryRun {
		return nil
	}
	if !o.yes && !confirm(w, in, fmt.Sprintf("\n执行以上 %d 项改动？替换的内容先备份到 %s [y/N] ", n, tilde(filepath.Join(m.Home, "skill-backups")))) {
		fmt.Fprintln(w, "已取消，没有改动。")
		return nil
	}
	opts.DryRun = false
	res, err := m.Sync(opts)
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	printActions(w, res.Actions, false)
	return finish(w, res, "同步", "已经打开的 Agent 会话一般要新开会话才会读到新的 Skill。")
}

func skillsRestore(w io.Writer, in io.Reader, m *skills.Manager, args []string) error {
	o, err := parseSkillFlags("restore", args)
	if err != nil {
		return err
	}
	opts := skills.RestoreOptions{Names: o.names, All: o.all, Locations: o.to, DryRun: true}
	plan, err := m.Restore(opts)
	if err != nil {
		return err
	}
	if len(plan.Actions) == 0 {
		fmt.Fprintln(w, "没有需要还原的同步记录。")
		return nil
	}
	printActions(w, plan.Actions, true)
	n := plan.Changes()
	if n == 0 {
		return errors.New("没有可以还原的条目")
	}
	if !o.yes && !confirm(w, in, fmt.Sprintf("\n还原以上 %d 项？[y/N] ", n)) {
		fmt.Fprintln(w, "已取消，没有改动。")
		return nil
	}
	opts.DryRun = false
	res, err := m.Restore(opts)
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	printActions(w, res.Actions, false)
	return finish(w, res, "还原", "MCPcell 来源目录里的副本保留在 "+tilde(m.SourceDir())+"，不需要时可以自行删除。")
}

func hasDo(acts []skills.Action, do string) bool {
	for _, a := range acts {
		if a.Do == do {
			return true
		}
	}
	return false
}

func finish(w io.Writer, res *skills.Result, verb, tip string) error {
	var done, failed, left int
	for _, a := range res.Actions {
		switch {
		case a.Error != "":
			failed++
		case a.Done:
			done++
		case a.Do == "conflict":
			left++
		}
	}
	fmt.Fprintf(w, "\n%s %d 项，失败 %d 项", verb, done, failed)
	if left > 0 {
		fmt.Fprintf(w, "，%d 项内容不同或被改动过，没有动", left)
	}
	fmt.Fprintf(w, "。%s\n", tip)
	if failed > 0 {
		return errors.New("部分改动没有完成")
	}
	return nil
}

// printActions lists a plan (or what was done) one line per action.
func printActions(w io.Writer, acts []skills.Action, planned bool) {
	for _, a := range acts {
		mark := "  "
		if !planned {
			switch {
			case a.Error != "":
				mark = "✗ "
			case a.Done:
				mark = "✓ "
			}
		}
		where := a.Location
		if where == skills.SourceID {
			where = "来源"
		}
		if where == "" {
			where = "-"
		}
		fmt.Fprintln(w, mark+pad(a.Skill, 24)+" "+pad(where, 16)+" "+actionText(a))
		if s := a.Diff.Summary(); s != "" && a.Do != "update-source" {
			fmt.Fprintf(w, "    %s%s\n", matchText(a.Match), s)
		}
		if a.Error != "" {
			fmt.Fprintf(w, "    %s\n", a.Error)
		}
	}
}

// pad fills s to n terminal columns.
func pad(s string, n int) string {
	if w := width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func matchText(m string) string {
	switch m {
	case "newer":
		return "这一份较新："
	case "older":
		return "这一份较旧："
	}
	return ""
}

func actionText(a skills.Action) string {
	link := "链接"
	if a.Copy {
		link = "副本"
	}
	switch a.Do {
	case "import":
		return "放进来源目录（取自 " + a.From + "）"
	case "update-source":
		s := "用 " + a.From + " 的版本替换来源目录里的版本（旧版本先备份）"
		if d := a.Diff.Summary(); d != "" {
			s += "：" + d
		}
		return s
	case "link":
		return "新建" + link
	case "replace":
		if a.Diff != nil {
			return "备份这一份不同的版本，改为" + link
		}
		return "备份后改为" + link
	case "update":
		return "更新 MCPcell 的副本"
	case "ok":
		return "已是最新"
	case "restore":
		return "还原"
	case "conflict":
		s := "内容不同，没有改动"
		if a.Reason != "" {
			s = a.Reason
		}
		if a.Variant != "" {
			s = "版本 " + a.Variant + "，" + s
		}
		return s
	}
	return "跳过：" + a.Reason
}
