package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/luohui1/MCP-cell/internal/home"
)

// status is the menu's lines about the daemon, in the page's words.
func status(dir string) []string {
	var st struct {
		Daemon struct {
			PrivateBytes uint64 `json:"privateBytes"`
		} `json:"daemon"`
		Services []struct {
			Name     string    `json:"name"`
			Of       string    `json:"of"`
			State    string    `json:"state"`
			Failures int       `json:"failures"`
			Instance *running  `json:"instance"`
			Runs     []running `json:"instances"`
		} `json:"services"`
		Sessions []struct{} `json:"sessions"`
	}
	if err := get(dir, "/api/status", &st); err != nil {
		return []string{"后台服务没有运行", "点「打开 MCPcell」会启动它"}
	}
	total, running, standby := st.Daemon.PrivateBytes, 0, 0
	var broken []string
	for _, s := range st.Services {
		runs := s.Runs
		if s.Instance != nil {
			runs = append(runs, *s.Instance)
		}
		for _, r := range runs {
			total += r.Usage.PrivateBytes
		}
		if s.Of != "" || strings.HasPrefix(s.Name, "run-") {
			continue // a variant counts with its service
		}
		switch {
		case s.Failures > 0 || s.State == "failed":
			broken = append(broken, s.Name)
		case len(runs) > 0:
			running++
		default:
			standby++
		}
	}
	lines := []string{fmt.Sprintf("运行中 %d 个，待命 %d 个 · 共 %s", running, standby, mb(total))}
	if n := len(st.Sessions); n > 0 {
		lines = append(lines, fmt.Sprintf("正连着 %d 个对话", n))
	}
	if len(broken) > 0 {
		lines = append(lines, "打不开："+strings.Join(broken, "、"))
	}
	return lines
}

type running struct {
	Usage struct {
		PrivateBytes uint64 `json:"privateBytes"`
	} `json:"usage"`
}

func mb(b uint64) string {
	v := float64(b) / (1 << 20)
	if v < 10 {
		return fmt.Sprintf("%.1f MB", v)
	}
	return fmt.Sprintf("%.0f MB", v)
}

// get is a minimal HTTP/1.0 GET to the daemon on loopback: the response
// has no chunked encoding and ends when the daemon closes the connection.
func get(dir, path string, out any) error {
	d, err := home.ReadDaemon(dir)
	if err != nil {
		return err
	}
	token, err := home.ReadToken(dir)
	if err != nil {
		return err
	}
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", d.Port), 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(c, "GET %s HTTP/1.0\r\nHost: 127.0.0.1:%d\r\nAuthorization: Bearer %s\r\n\r\n", path, d.Port, token)
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		return err
	}
	if f := strings.Fields(line); len(f) < 2 || f[1] != "200" {
		return errors.New("守护进程返回 " + strings.TrimSpace(line))
	}
	for { // headers
		h, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.TrimSpace(h) == "" {
			break
		}
	}
	return json.NewDecoder(io.LimitReader(br, 8<<20)).Decode(out)
}
