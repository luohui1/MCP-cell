package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/luohui1/MCP-cell/internal/registry"
)

// daemonCall talks to the running daemon's management API.
func daemonCall(method, path string, out any) error {
	home, err := registry.Home()
	if err != nil {
		return err
	}
	info, err := registry.ReadDaemonInfo(home)
	if err != nil {
		return errors.New("守护进程没有运行。启动：mcpcell start")
	}
	token, err := registry.Token(home)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", info.Port, path), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	timeout := 5 * time.Second
	if strings.HasSuffix(path, "/update") {
		timeout = 10 * time.Minute // npm install
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return fmt.Errorf("守护进程（PID %d，端口 %d）没有响应；可能已异常退出，请重新运行 mcpcell daemon", info.PID, info.Port)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("守护进程返回 %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type psStatus struct {
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"startedAt"`
	Daemon    struct {
		PrivateBytes uint64 `json:"privateBytes"`
	} `json:"daemon"`
	Services []struct {
		Name      string     `json:"name"`
		Mode      string     `json:"mode"`
		State     string     `json:"state"`
		LastError string     `json:"lastError"`
		RetryAt   *time.Time `json:"retryAt"`
		Catalog   *struct {
			Tools int `json:"tools"`
		} `json:"catalog"`
		Package *struct {
			Version string `json:"version"`
		} `json:"package"`
		Instance  *psInstance  `json:"instance"`
		Instances []psInstance `json:"instances"`
	} `json:"services"`
	Sessions []struct {
		Service string `json:"service"`
	} `json:"sessions"`
}

type psInstance struct {
	LastActiveAt time.Time `json:"lastActiveAt"`
	Inflight     int       `json:"inflight"`
	Usage        struct {
		Processes    int    `json:"processes"`
		PrivateBytes uint64 `json:"privateBytes"`
	} `json:"usage"`
}

func runPS() error {
	var st psStatus
	if err := daemonCall("GET", "/api/status", &st); err != nil {
		return err
	}
	fmt.Printf("MCPcell %s · PID %d · 127.0.0.1:%d · 已运行 %s · 守护进程 %s\n\n",
		st.Version, st.PID, st.Port, human(time.Since(st.StartedAt)), mb(st.Daemon.PrivateBytes))
	if len(st.Services) == 0 {
		fmt.Println("还没有服务。在 ~/.mcpcell/config.json 的 services 里添加后重启守护进程。")
		return nil
	}
	sessions := map[string]int{}
	for _, s := range st.Sessions {
		sessions[s.Service]++
	}
	rows := [][]string{{"服务", "状态", "会话", "进程", "私有提交", "工具", "活动"}}
	var total uint64
	for _, s := range st.Services {
		state, procs, mem, act := map[string]string{"ready": "运行中", "starting": "启动中", "failed": "启动失败"}[s.State], "-", "-", "-"
		if state == "" {
			state = "未启动"
		}
		runs := s.Instances
		if s.Instance != nil {
			runs = append(runs, *s.Instance)
		}
		perSession := s.Mode == "per-session"
		if len(runs) > 0 {
			var n int
			var bytes uint64
			var inflight int
			var last time.Time
			for _, in := range runs {
				n, bytes, inflight = n+in.Usage.Processes, bytes+in.Usage.PrivateBytes, inflight+in.Inflight
				if in.LastActiveAt.After(last) {
					last = in.LastActiveAt
				}
			}
			procs, mem = fmt.Sprint(n), mb(bytes)
			total += bytes
			act = "空闲 " + human(time.Since(last))
			if inflight > 0 {
				act = fmt.Sprintf("处理中 %d", inflight)
			}
			if perSession {
				act = fmt.Sprintf("%d 个会话实例 · %s", len(s.Instances), act)
			}
		} else if s.RetryAt != nil {
			act = human(time.Until(*s.RetryAt)) + "后可重试"
		} else if s.State == "cold" {
			act = "调用时启动"
			if perSession {
				act = "按会话 · 调用时启动"
			}
		}
		tools := "-"
		if s.Catalog != nil {
			tools = fmt.Sprint(s.Catalog.Tools)
		}
		rows = append(rows, []string{s.Name, state, fmt.Sprint(sessions[s.Name]), procs, mem, tools, act})
	}
	printTable(rows)
	fmt.Printf("\n会话 %d · MCP 私有提交合计 %s\n", len(st.Sessions), mb(total))
	for _, s := range st.Services {
		if s.State == "failed" && s.LastError != "" {
			line, _, _ := strings.Cut(s.LastError, "\n")
			fmt.Printf("  %s：%s\n", s.Name, line)
		}
	}
	return nil
}

func runUpdate(service string) error {
	fmt.Printf("正在更新 %s 的 npm 包……\n", service)
	var out struct{ Version, Previous, Error string }
	if err := daemonCall("POST", "/api/services/"+service+"/update", &out); err != nil {
		return err
	}
	switch {
	case out.Previous == out.Version:
		fmt.Printf("%s 已是最新：%s\n", service, out.Version)
	case out.Previous == "":
		fmt.Printf("%s 已安装并锁定：%s\n", service, out.Version)
	default:
		fmt.Printf("%s 已从 %s 更新到 %s\n", service, out.Previous, out.Version)
	}
	if out.Error != "" {
		return errors.New(out.Error)
	}
	return nil
}

func mb(b uint64) string {
	if b == 0 {
		return "-"
	}
	v := float64(b) / (1 << 20)
	if v < 10 {
		return fmt.Sprintf("%.1f MB", v)
	}
	return fmt.Sprintf("%.0f MB", v)
}

func human(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d 小时", int(d.Hours()))
	}
	return fmt.Sprintf("%d 天", int(d.Hours()/24))
}

// printTable pads by display width so CJK columns line up in a terminal.
func printTable(rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], width(c))
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r {
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-width(c)+2))
			}
		}
		fmt.Println(b.String())
	}
}

func width(s string) int {
	w := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0xFF00 && r <= 0xFFEF) || (r >= 0x3000 && r <= 0x303F) {
			w += 2
		} else {
			w++
		}
	}
	return w
}
