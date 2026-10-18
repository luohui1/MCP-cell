// Command mcpprobe connects N concurrent MCP clients (official Go SDK) and
// times connect, tools/list and one tool call per client. It talks to an
// MCPcell endpoint, or with -native it starts the server itself over stdio
// the way a client would, for before/after comparisons.
//
//	go run ./tools/mcpprobe -service context7 -n 6 -tool resolve-library-id -args '{"libraryName":"react"}'
//	go run ./tools/mcpprobe -native native.json -service context7 -json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
)

type authRT struct{ token string }

func (a authRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

// result is one client's timings, printed as a JSON line with -json.
type result struct {
	Client    int     `json:"client"`
	OK        bool    `json:"ok"`
	ConnectMS float64 `json:"connectMs"`
	ListMS    float64 `json:"listMs"`
	CallMS    float64 `json:"callMs,omitempty"`
	Tools     int     `json:"tools"`
	Protocol  string  `json:"protocol,omitempty"`
	Output    string  `json:"output,omitempty"`
	Error     string  `json:"error,omitempty"`
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func main() {
	service := flag.String("service", "", "service name (MCPcell) or entry name in -native")
	n := flag.Int("n", 1, "concurrent clients")
	tool := flag.String("tool", "", "tool to call from every client (empty: only list tools)")
	args := flag.String("args", "{}", "tool arguments as JSON")
	hold := flag.Duration("hold", 0, "keep sessions open this long before closing (to inspect mcpcell ps)")
	native := flag.String("native", "", "mcpServers JSON file: start the entry over stdio instead of using MCPcell")
	asJSON := flag.Bool("json", false, "print one JSON line per client")
	showStderr := flag.Bool("stderr", false, "with -native, copy the server's stderr to ours")
	flag.Parse()

	var arguments map[string]any
	check(json.Unmarshal([]byte(*args), &arguments))
	connect := mcpcellTransport(*service)
	if *native != "" {
		connect = nativeTransport(*native, *service, *showStderr)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var cleanups []func()
	results := make([]result, *n)
	for i := range *n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := &results[i]
			r.Client = i
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			t0 := time.Now()
			transport, cleanup, err := connect()
			if err != nil {
				r.Error = err.Error()
				return
			}
			client := mcp.NewClient(&mcp.Implementation{Name: "mcpprobe", Version: "1"}, nil)
			cs, err := client.Connect(ctx, transport, nil)
			if err != nil {
				r.Error = "connect: " + err.Error()
				cleanup()
				return
			}
			mu.Lock()
			cleanups = append(cleanups, func() { cs.Close(); cleanup() })
			mu.Unlock()
			r.ConnectMS, r.Protocol = ms(time.Since(t0)), cs.InitializeResult().ProtocolVersion
			t1 := time.Now()
			tools, err := cs.ListTools(ctx, nil)
			if err != nil {
				r.Error = "list: " + err.Error()
				return
			}
			r.ListMS, r.Tools = ms(time.Since(t1)), len(tools.Tools)
			if *tool != "" {
				t2 := time.Now()
				res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: *tool, Arguments: arguments})
				if err != nil {
					r.Error = "call: " + err.Error()
					return
				}
				r.CallMS = ms(time.Since(t2))
				if len(res.Content) > 0 {
					if tc, ok := res.Content[0].(*mcp.TextContent); ok {
						r.Output = strings.Join(strings.Fields(tc.Text), " ")
						if len([]rune(r.Output)) > 70 {
							r.Output = string([]rune(r.Output)[:70]) + "…"
						}
					}
				}
			}
			r.OK = true
		}()
	}
	wg.Wait()
	for _, r := range results {
		if *asJSON {
			b, _ := json.Marshal(r)
			fmt.Println(string(b))
			continue
		}
		if !r.OK {
			fmt.Printf("client %d: %s\n", r.Client, r.Error)
			continue
		}
		line := fmt.Sprintf("client %d: protocol %s, %d tools, connect %.0fms, list %.0fms", r.Client, r.Protocol, r.Tools, r.ConnectMS, r.ListMS)
		if *tool != "" {
			line += fmt.Sprintf(", call %.0fms: %s", r.CallMS, r.Output)
		}
		fmt.Println(line)
	}
	if *hold > 0 {
		fmt.Fprintf(os.Stderr, "holding %d sessions for %v\n", *n, *hold)
		time.Sleep(*hold)
	}
	for _, c := range cleanups {
		c()
	}
}

func mcpcellTransport(service string) func() (mcp.Transport, func(), error) {
	home, err := registry.Home()
	check(err)
	info, err := registry.ReadDaemonInfo(home)
	check(err)
	token, err := registry.Token(home)
	check(err)
	url := fmt.Sprintf("http://127.0.0.1:%d/s/%s", info.Port, service)
	return func() (mcp.Transport, func(), error) {
		return &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: authRT{token}}}, func() {}, nil
	}
}

// nativeTransport starts the entry the way a client does (full environment
// plus the entry's env), inside a Job Object so the whole npx tree is killed
// afterwards.
func nativeTransport(file, name string, showStderr bool) func() (mcp.Transport, func(), error) {
	raw, err := os.ReadFile(file)
	check(err)
	var doc struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	check(json.Unmarshal(raw, &doc))
	e, ok := doc.MCPServers[name]
	if !ok {
		check(fmt.Errorf("%s 里没有 %s", file, name))
	}
	env := os.Environ()
	for k, v := range e.Env {
		env = append(env, k+"="+v)
	}
	dir, _ := os.UserHomeDir()
	return func() (mcp.Transport, func(), error) {
		p, err := proc.Start(proc.Spec{Argv: append([]string{e.Command}, e.Args...), Env: env, Dir: dir})
		if err != nil {
			return nil, nil, err
		}
		if showStderr {
			go func() { _, _ = io.Copy(os.Stderr, p.Stderr) }()
		}
		return &mcp.IOTransport{Reader: p.Stdout, Writer: p.Stdin}, p.Kill, nil
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcpprobe:", err)
		os.Exit(1)
	}
}
