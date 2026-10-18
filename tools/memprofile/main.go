// Command memprofile starts one MCPcell service the way the daemon does
// (`node <bin>` for a pinned npm package) under different node flag sets and
// reports private commit after initialize, tools/list and an optional call.
//
//	go run ./tools/memprofile -service context7 -tool resolve-library-id -args '{"libraryName":"react","query":"x"}' \
//	  -variants 'default|--max-semi-space-size=1|--max-semi-space-size=1 --optimize-for-size'
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
)

type sample struct {
	ok              bool
	err             string
	privMB, wsMB    float64
	output          string
	startMS, callMS float64
}

func main() {
	service := flag.String("service", "", "service name in ~/.mcpcell/config.json")
	tool := flag.String("tool", "", "tool to call once (empty: only list tools)")
	args := flag.String("args", "{}", "tool arguments as JSON")
	variants := flag.String("variants", "default", "'|'-separated node flag sets; 'default' means none; env:K=V sets an env var")
	repeat := flag.Int("repeat", 2, "runs per variant (median reported)")
	settle := flag.Duration("settle", 3*time.Second, "wait after the last request before measuring")
	flag.Parse()

	home, err := registry.Home()
	check(err)
	cfg, err := registry.Load(home)
	check(err)
	def := cfg.Services[*service]
	if def == nil {
		check(fmt.Errorf("没有服务 %s", *service))
	}
	pkgs, err := npmcache.Open(filepath.Join(home, "pkgs"), nil)
	check(err)
	argv := proc.UnwrapCmd(append([]string{def.Command}, def.Args...))
	spec, rest, ok := npmcache.Detect(argv)
	if !ok {
		check(fmt.Errorf("%s 不是 npx 启动的 npm 包", *service))
	}
	pin := pkgs.Pinned(spec)
	if pin == nil {
		check(fmt.Errorf("%s 还没有安装到包缓存，先在 MCPcell 里启动一次", *service))
	}
	baseEnv := proc.BuildEnv(os.Environ(), def.Env)
	node, err := proc.ResolveArgv([]string{"node"}, baseEnv, home)
	check(err)
	var arguments map[string]any
	check(json.Unmarshal([]byte(*args), &arguments))
	userHome, _ := os.UserHomeDir()

	fmt.Printf("%s %s@%s，每组 %d 次取中位数\n\n", *service, pin.Name, pin.Version, *repeat)
	fmt.Printf("%-62s %4s %9s %8s %9s %8s\n", "node 参数", "成功", "私有提交", "工作集", "启动(ms)", "调用(ms)")
	for _, v := range strings.Split(*variants, "|") {
		var flags []string
		env := append([]string(nil), baseEnv...)
		for _, f := range strings.Fields(v) {
			switch {
			case f == "default":
			case strings.HasPrefix(f, "env:"):
				env = append(env, strings.TrimPrefix(f, "env:"))
			default:
				flags = append(flags, f)
			}
		}
		full := append(append(append([]string{node[0]}, flags...), pin.Script()), rest...)
		var runs []sample
		for range *repeat {
			runs = append(runs, measure(proc.Spec{Argv: full, Env: env, Dir: userHome}, *tool, arguments, *settle))
		}
		report(v, runs)
	}
}

func measure(spec proc.Spec, tool string, arguments map[string]any, settle time.Duration) sample {
	t0 := time.Now()
	p, err := proc.Start(spec)
	if err != nil {
		return sample{err: err.Error()}
	}
	defer p.Kill()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "memprofile", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.IOTransport{Reader: p.Stdout, Writer: p.Stdin}, nil)
	if err != nil {
		return sample{err: "connect: " + err.Error()}
	}
	defer cs.Close()
	if _, err := cs.ListTools(ctx, nil); err != nil {
		return sample{err: "list: " + err.Error()}
	}
	s := sample{startMS: float64(time.Since(t0).Milliseconds())}
	if tool != "" {
		t1 := time.Now()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
		if err != nil {
			return sample{err: "call: " + err.Error()}
		}
		if res.IsError {
			return sample{err: "tool returned an error"}
		}
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				s.output = strings.Join(strings.Fields(tc.Text), " ")
				if len([]rune(s.output)) > 80 {
					s.output = string([]rune(s.output)[:80]) + "…"
				}
			}
		}
		s.callMS = float64(time.Since(t1).Milliseconds())
	}
	time.Sleep(settle)
	u, err := p.Usage()
	if err != nil {
		return sample{err: "usage: " + err.Error()}
	}
	s.ok, s.privMB, s.wsMB = true, float64(u.PrivateBytes)/(1<<20), float64(u.WorkingSetBytes)/(1<<20)
	return s
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	return xs[len(xs)/2]
}

func report(name string, runs []sample) {
	var priv, ws, start, call []float64
	errs, out := "", ""
	for _, r := range runs {
		out = r.output
		if !r.ok {
			errs = r.err
			continue
		}
		priv, ws, start, call = append(priv, r.privMB), append(ws, r.wsMB), append(start, r.startMS), append(call, r.callMS)
	}
	fmt.Printf("%-62s %d/%d %7.1fMB %6.1fMB %9.0f %8.0f %s\n", name, len(priv), len(runs),
		median(priv), median(ws), median(start), median(call), errs)
	if out != "" {
		fmt.Printf("    返回：%s\n", out)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "memprofile:", err)
		os.Exit(1)
	}
}
