// Package fakemcp is a stdio MCP server built on the official Go SDK, used as
// an independent upstream in tests. Test binaries re-exec themselves with
// MCPCELL_FAKE_MCP=1 to become this server; see MaybeRun and Spec.
package fakemcp

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/luohui1/MCP-cell/internal/proc"
)

const envVar = "MCPCELL_FAKE_MCP"

// MaybeRun turns the current process into the fake server when asked to.
// Call it first thing in TestMain.
func MaybeRun() {
	if os.Getenv(envVar) != "1" {
		return
	}
	_ = newServer().Run(context.Background(), &mcp.StdioTransport{})
	os.Exit(0)
}

// Spec launches the running test binary as the fake server.
func Spec() proc.Spec { return SpecWith(0) }

// SpecWith sets the server's list page size, to exercise pagination.
func SpecWith(pageSize int) proc.Spec {
	self, _ := os.Executable()
	return proc.Spec{
		Argv: []string{self},
		Env:  proc.BuildEnv(os.Environ(), map[string]string{envVar: "1", pageEnv: strconv.Itoa(pageSize)}),
		Dir:  os.TempDir(),
	}
}

const pageEnv = "MCPCELL_FAKE_PAGE"

type echoIn struct {
	Text string `json:"text,omitempty"`
}

type slowIn struct {
	MS    int `json:"ms,omitempty"`
	Steps int `json:"steps,omitempty"`
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func newServer() *mcp.Server {
	page, _ := strconv.Atoi(os.Getenv(pageEnv))
	s := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1.0.0"}, &mcp.ServerOptions{PageSize: page})
	s.AddPrompt(&mcp.Prompt{Name: "greet", Description: "Say hello"},
		func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "hello"}}}}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "pid", Description: "Return the server process id"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return text(strconv.Itoa(os.Getpid())), nil, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "echo", Description: "Echo text back"},
		func(ctx context.Context, req *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
			return text(in.Text), nil, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "slow", Description: "Sleep ms, reporting progress in steps"},
		func(ctx context.Context, req *mcp.CallToolRequest, in slowIn) (*mcp.CallToolResult, any, error) {
			steps := max(in.Steps, 1)
			token := req.Params.GetProgressToken()
			for n := 1; n <= steps; n++ {
				select {
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				case <-time.After(time.Duration(in.MS/steps) * time.Millisecond):
				}
				if token != nil {
					_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
						ProgressToken: token, Progress: float64(n), Total: float64(steps)})
				}
			}
			return text("done"), nil, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "crash", Description: "Exit the server process"},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			os.Exit(3)
			return nil, nil, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "add_tool", Description: "Add a tool, which emits tools/list_changed"},
		func(ctx context.Context, req *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
			mcp.AddTool(s, &mcp.Tool{Name: "extra_" + in.Text},
				func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
					return text("extra"), nil, nil
				})
			return text("added"), nil, nil
		})
	return s
}
