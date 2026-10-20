package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/luohui1/MCP-cell/internal/registry"
)

// mcpView is GET /api/mcp: every MCP across MCPcell's services and the
// agents' configurations, the data the management page's one-click
// takeover works from.
type mcpView struct {
	Agents    []agentView  `json:"agents"`
	Rows      []mcpRowView `json:"rows"`
	Autostart struct {
		Supported, Enabled bool
	} `json:"autostart"`
}

type agentView struct {
	ID, Name, Path, Error string
	Found                 bool
}

type mcpRowView struct {
	Name, Mode, Class, Reason, Service, Proposed string
	Agents                                       map[string]cellView
}

type cellView struct {
	State, Entry, Class, Reason string
	Takeable                    bool
}

// takeoverOptions narrow a takeover and choose how new services run.
type takeoverOptions struct {
	clients []string        // client IDs or names; none means all
	entries map[string]bool // entry or service names; none means all
	mode    string          // "", registry.ModeShared or registry.ModePerSession
}

type takeStep struct {
	agent   agentView
	entry   string
	service string
	isNew   bool // the service is created by this takeover
	mode    string
	note    string
}

type skipStep struct {
	agent agentView
	entry string // "" for the whole agent
	why   string
}

type takeoverPlan struct {
	steps     []takeStep
	skipped   []skipStep
	managed   map[string][]string // already taken over: entry names by agent ID
	unmatched []string            // entry filters that matched nothing
	agents    []agentView         // the agents considered, in order
}

var modeName = map[string]string{registry.ModeShared: "共享", registry.ModePerSession: "按会话"}

var takeoverHint = map[string]string{
	"claude-code": "Claude Code 新会话立即生效，已打开的会话重启后生效",
	"codex":       "Codex（包括桌面版）重启后生效",
	"cursor":      "在 Cursor 的 MCP 设置里刷新，或重启 Cursor",
}

// runTakeover is the command line's one-click takeover: it shows the plan,
// asks, and has the daemon do the takeover through the same API the
// management page uses.
func runTakeover(args []string) error {
	return takeoverWith(os.Stdout, os.Stdin, args)
}

func takeoverWith(w io.Writer, in io.Reader, args []string) error {
	o, yes, err := parseTakeover(args)
	if err != nil {
		return err
	}
	if _, _, err := ensureDaemon(); err != nil {
		return err
	}
	var v mcpView
	if err := daemonCall("GET", "/api/mcp", &v); err != nil {
		return err
	}
	if o.clients, err = resolveClients(v.Agents, o.clients); err != nil {
		return err
	}
	p := planTakeover(v, o)
	printPlan(w, p)
	if len(p.steps) == 0 {
		if len(o.entries) > 0 && len(p.unmatched) == len(o.entries) {
			return errors.New("没有找到要托管的条目，可以用 mcpcell clients 查看条目名")
		}
		fmt.Fprintln(w, "没有需要托管的条目。")
		return nil
	}
	if !yes && !confirm(w, in, fmt.Sprintf("托管以上 %d 个条目？[y/N] ", len(p.steps))) {
		fmt.Fprintln(w, "已取消，没有改动。")
		return nil
	}
	return applyPlan(w, p, o.mode, v.Autostart.Supported && !v.Autostart.Enabled)
}

// listFlag collects a repeatable, comma-separated flag.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error {
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			*l = append(*l, v)
		}
	}
	return nil
}

// parseTakeover accepts flags before, between and after entry names.
func parseTakeover(args []string) (o takeoverOptions, yes bool, err error) {
	fs := flag.NewFlagSet("takeover", flag.ContinueOnError)
	fs.BoolVar(&yes, "y", false, "不询问，直接执行")
	fs.BoolVar(&yes, "yes", false, "同 -y")
	shared := fs.Bool("shared", false, "未验证的 MCP 也共享（需要会话隔离的仍按会话）")
	perSession := fs.Bool("per-session", false, "新建的服务一律每个会话一个实例")
	var clientList, names listFlag
	fs.Var(&clientList, "client", "只处理这些客户端：claude-code、codex、cursor（可重复或用逗号分隔）")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "用法: mcpcell takeover [条目...] [--client 客户端] [--shared|--per-session] [-y]")
		fs.PrintDefaults()
	}
	for {
		if err := fs.Parse(args); err != nil {
			return o, false, err
		}
		if fs.NArg() == 0 {
			break
		}
		_ = names.Set(fs.Arg(0))
		args = fs.Args()[1:]
	}
	switch {
	case *shared && *perSession:
		return o, false, errors.New("--shared 和 --per-session 只能选一个")
	case *shared:
		o.mode = registry.ModeShared
	case *perSession:
		o.mode = registry.ModePerSession
	}
	o.clients = clientList
	if len(names) > 0 {
		o.entries = map[string]bool{}
		for _, n := range names {
			o.entries[n] = true
		}
	}
	return o, yes, nil
}

// resolveClients turns client IDs or names into IDs.
func resolveClients(agents []agentView, want []string) ([]string, error) {
	var ids, known []string
	for _, a := range agents {
		known = append(known, a.ID)
	}
	for _, w := range want {
		found := ""
		for _, a := range agents {
			if strings.EqualFold(w, a.ID) || strings.EqualFold(w, a.Name) {
				found = a.ID
			}
		}
		if found == "" {
			return nil, fmt.Errorf("未知客户端 %q，可选：%s", w, strings.Join(known, "、"))
		}
		ids = append(ids, found)
	}
	return ids, nil
}

// planTakeover decides, entry by entry, what a takeover would do, the same
// way the management page's one-click dialog does.
func planTakeover(v mcpView, o takeoverOptions) takeoverPlan {
	p := takeoverPlan{managed: map[string][]string{}}
	matched := map[string]bool{}
	for _, a := range v.Agents {
		if len(o.clients) > 0 && !contains(o.clients, a.ID) {
			continue
		}
		p.agents = append(p.agents, a)
		switch {
		case a.Error != "":
			p.skipped = append(p.skipped, skipStep{agent: a, why: a.Error})
			continue
		case !a.Found:
			p.skipped = append(p.skipped, skipStep{agent: a, why: "没有找到配置"})
			continue
		}
		var steps []takeStep
		for _, r := range v.Rows {
			c, ok := r.Agents[a.ID]
			if !ok {
				continue
			}
			if len(o.entries) > 0 {
				hit := ""
				for _, n := range []string{c.Entry, r.Service, r.Proposed} {
					if n != "" && o.entries[n] {
						hit = n
						break
					}
				}
				if hit == "" {
					continue
				}
				matched[hit] = true
			}
			switch {
			case c.State == "managed":
				p.managed[a.ID] = append(p.managed[a.ID], c.Entry)
			case c.State == "native" && c.Takeable:
				steps = append(steps, planStep(a, r, c, o.mode))
			case c.State == "changed":
				p.skipped = append(p.skipped, skipStep{a, c.Entry, "托管后被改过：先用 mcpcell restore 还原，或在管理页忘记托管记录"})
			default:
				why := c.Reason
				if why == "" {
					why = r.Reason
				}
				p.skipped = append(p.skipped, skipStep{a, c.Entry, why})
			}
		}
		sort.SliceStable(steps, func(i, j int) bool { return steps[i].entry < steps[j].entry })
		p.steps = append(p.steps, steps...)
		sort.Strings(p.managed[a.ID])
	}
	for n := range o.entries {
		if !matched[n] {
			p.unmatched = append(p.unmatched, n)
		}
	}
	sort.Strings(p.unmatched)
	return p
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// planStep is how one entry is taken over. An existing service keeps its
// mode; a new one is shared when the server is known to keep no per-session
// state (or, with --shared, when it is merely unverified), and otherwise
// runs one instance per session, like the agent ran it.
func planStep(a agentView, r mcpRowView, c cellView, mode string) takeStep {
	s := takeStep{agent: a, entry: c.Entry, service: r.Service}
	if s.service != "" {
		s.mode, s.note = r.Mode, "沿用 MCPcell 里已有的服务"
		if mode != "" && mode != r.Mode {
			s.note += "，运行方式不变（在 config.json 的 mode 里改）"
		}
		return s
	}
	s.isNew, s.service = true, r.Proposed
	if s.service == "" {
		s.service = c.Entry
	}
	switch {
	case mode == registry.ModePerSession:
		s.mode, s.note = registry.ModePerSession, "按 --per-session 每个会话一个实例"
	case c.Class == "shared":
		s.mode, s.note = registry.ModeShared, c.Reason
	case c.Class == "unknown" && mode == registry.ModeShared:
		s.mode, s.note = registry.ModeShared, "未验证，按 --shared 共享"
	case c.Class == "unknown":
		s.mode, s.note = registry.ModePerSession, "未验证，默认按会话；确认它不保存会话状态可以加 --shared"
	default:
		s.mode, s.note = registry.ModePerSession, c.Reason
		if mode == registry.ModeShared {
			s.note = "需要会话隔离，不共享：" + c.Reason
		}
	}
	return s
}

func printPlan(w io.Writer, p takeoverPlan) {
	if len(p.steps) > 0 {
		fmt.Fprintf(w, "托管计划：%d 个条目\n", len(p.steps))
		created := map[string]bool{}
		var rows [][]string
		var agent string
		flush := func() {
			if len(rows) > 0 {
				fprintTable(w, rows)
				rows = nil
			}
		}
		for _, s := range p.steps {
			if s.agent.ID != agent {
				flush()
				agent = s.agent.ID
				fmt.Fprintf(w, "\n%s  %s\n", s.agent.Name, s.agent.Path)
			}
			svc := "服务 " + s.service
			if s.isNew && !created[s.service] {
				svc, created[s.service] = "新建服务 "+s.service, true
			}
			rows = append(rows, []string{"  " + s.entry, modeName[s.mode], svc, s.note})
		}
		flush()
	}
	if len(p.managed) > 0 {
		var managed []string
		for _, a := range p.agents {
			if names := p.managed[a.ID]; len(names) > 0 {
				managed = append(managed, a.Name+"："+strings.Join(names, "、"))
			}
		}
		fmt.Fprintf(w, "\n已托管，不再处理：%s\n", strings.Join(managed, "；"))
	}
	if len(p.skipped) > 0 {
		fmt.Fprintln(w, "\n跳过：")
		rows := make([][]string, 0, len(p.skipped))
		for _, s := range p.skipped {
			what := "  " + s.agent.Name
			if s.entry != "" {
				what += " / " + s.entry
			}
			rows = append(rows, []string{what, s.why})
		}
		fprintTable(w, rows)
	}
	if len(p.unmatched) > 0 {
		fmt.Fprintf(w, "\n没有找到这些条目：%s\n", strings.Join(p.unmatched, "、"))
	}
	if len(p.steps) > 0 {
		var files []string
		last := ""
		for _, s := range p.steps {
			if s.agent.ID != last {
				files, last = append(files, s.agent.Path), s.agent.ID
			}
		}
		fmt.Fprintf(w, "\n将修改：\n  %s\n", strings.Join(files, "\n  "))
		fmt.Fprintln(w, "只改上面列出的条目，改之前先备份整个文件；随时可以用 mcpcell restore 还原。")
		fmt.Fprintln(w, "MCPcell 会先试着启动每个 MCP，启动不了的条目保持原样。")
		fmt.Fprintln(w)
	}
}

func confirm(w io.Writer, in io.Reader, prompt string) bool {
	fmt.Fprint(w, prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "是":
		return true
	}
	return false
}

type attachResult struct {
	Name, Service, Error string
	OK, Created          bool
}

// applyPlan has the daemon take the planned entries over, one request per
// agent, and reports every entry.
func applyPlan(w io.Writer, p takeoverPlan, mode string, offerAutostart bool) error {
	fmt.Fprintln(w, "正在托管：MCPcell 先确认每个 MCP 能在它下面启动，第一次可能要安装 npm 包……")
	ok, failed := 0, 0
	var done []agentView
	for _, a := range p.agents {
		var names []string
		for _, s := range p.steps {
			if s.agent.ID == a.ID {
				names = append(names, s.entry)
			}
		}
		if len(names) == 0 {
			continue
		}
		req := map[string]any{"names": names, "force": mode == registry.ModeShared, "mode": mode}
		var out struct{ Results []attachResult }
		if err := daemonSend("POST", "/api/clients/"+url.PathEscape(a.ID)+"/attach", req, &out); err != nil {
			failed += len(names)
			fmt.Fprintf(w, "  ✗ %s：%v\n", a.Name, err)
			continue
		}
		n := 0
		for _, r := range out.Results {
			if !r.OK {
				failed++
				fmt.Fprintf(w, "  ✗ %s / %s：%s\n", a.Name, r.Name, r.Error)
				continue
			}
			ok, n = ok+1, n+1
			how := "服务 " + r.Service
			if r.Created {
				how = "新建服务 " + r.Service
			}
			fmt.Fprintf(w, "  ✓ %s / %s → %s\n", a.Name, r.Name, how)
		}
		if n > 0 {
			done = append(done, a)
		}
	}
	fmt.Fprintf(w, "\n已托管 %d 个，失败 %d 个。\n", ok, failed)
	for _, a := range done {
		hint := takeoverHint[a.ID]
		if hint == "" {
			hint = a.Name + " 重启后生效"
		}
		fmt.Fprintln(w, "  "+hint)
	}
	if ok > 0 && offerAutostart {
		fmt.Fprintln(w, "建议开启开机自启，让托管的 MCP 随时可用：mcpcell autostart on")
	}
	if failed > 0 {
		return errors.New("部分条目没有托管")
	}
	return nil
}
