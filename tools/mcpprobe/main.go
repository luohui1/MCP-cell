// Command mcpprobe connects N concurrent MCP clients (official Go SDK) to an
// MCPcell endpoint and calls one tool from each, for acceptance checks against
// real servers.
//
//	go run ./tools/mcpprobe -service context7 -n 6 -tool resolve-library-id -args '{"libraryName":"react"}'
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/luohui1/MCP-cell/internal/registry"
)

type authRT struct{ token string }

func (a authRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func main() {
	service := flag.String("service", "", "service name in MCPcell")
	n := flag.Int("n", 6, "concurrent clients")
	tool := flag.String("tool", "", "tool to call from every client (empty: only list tools)")
	args := flag.String("args", "{}", "tool arguments as JSON")
	hold := flag.Duration("hold", 0, "keep sessions open this long before closing (to inspect mcpcell ps)")
	flag.Parse()

	home, err := registry.Home()
	check(err)
	info, err := registry.ReadDaemonInfo(home)
	check(err)
	token, err := registry.Token(home)
	check(err)
	var arguments map[string]any
	check(json.Unmarshal([]byte(*args), &arguments))
	url := fmt.Sprintf("http://127.0.0.1:%d/s/%s", info.Port, *service)

	var wg sync.WaitGroup
	sessions := make([]*mcp.ClientSession, *n)
	for i := range *n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			client := mcp.NewClient(&mcp.Implementation{Name: "mcpprobe", Version: "1"}, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url,
				HTTPClient: &http.Client{Transport: authRT{token}}}, nil)
			if err != nil {
				fmt.Printf("client %d: connect failed: %v\n", i, err)
				return
			}
			sessions[i] = cs
			connected := time.Since(t0)
			tools, err := cs.ListTools(ctx, nil)
			if err != nil {
				fmt.Printf("client %d: list tools failed: %v\n", i, err)
				return
			}
			line := fmt.Sprintf("client %d: protocol %s, %d tools, connect %v", i,
				cs.InitializeResult().ProtocolVersion, len(tools.Tools), connected.Round(time.Millisecond))
			if *tool != "" {
				t1 := time.Now()
				res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: *tool, Arguments: arguments})
				switch {
				case err != nil:
					line += fmt.Sprintf(", call failed: %v", err)
				case len(res.Content) > 0:
					text := ""
					if tc, ok := res.Content[0].(*mcp.TextContent); ok {
						text = strings.Join(strings.Fields(tc.Text), " ")
					}
					if len([]rune(text)) > 70 {
						text = string([]rune(text)[:70]) + "…"
					}
					line += fmt.Sprintf(", call %v: %s", time.Since(t1).Round(time.Millisecond), text)
				}
			}
			fmt.Println(line)
		}()
	}
	wg.Wait()
	if *hold > 0 {
		fmt.Printf("holding %d sessions for %v\n", *n, *hold)
		time.Sleep(*hold)
	}
	for _, cs := range sessions {
		if cs != nil {
			cs.Close()
		}
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcpprobe:", err)
		os.Exit(1)
	}
}
