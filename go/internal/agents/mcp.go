package agents

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Stable client metadata is required for every call, including HTTP retries.
// It identifies an intent, never company, identity or delegated permission.
const MCPCallIDKey = "io.erp-town/call-id"

func NewMCPServer(ctx context.Context, x *PolicyExecutor) (*mcp.Server, error) {
	if x == nil || x.Validate(ctx) != nil {
		return nil, ErrInvalidRun
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "ERP Town draft assistant", Version: "1"}, nil)
	for _, tool := range x.Tools() {
		server.AddTool(&mcp.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, _ := req.Params.GetMeta()[MCPCallIDKey].(string)
			out := policyStop("CALL_ID_REQUIRED")
			if validID(id) {
				var err error
				out, err = x.Execute(ctx, Call{ID: id, Tool: req.Params.Name, Arguments: req.Params.Arguments})
				if err != nil {
					out = policyStop("POLICY_UNAVAILABLE")
				}
			}
			return &mcp.CallToolResult{IsError: out.NeedsHuman, StructuredContent: out.Output, Content: []mcp.Content{&mcp.TextContent{Text: string(out.Output)}}}, nil
		})
	}
	return server, nil
}

// MCPResolver is a host authentication boundary. It must resolve a configured
// agent run for the authenticated requester, never bind authority from JSON.
type MCPResolver func(*http.Request) (*PolicyExecutor, error)

func NewMCPHandler(resolve MCPResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if resolve == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		x, err := resolve(r)
		if err != nil || x == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		server, err := NewMCPServer(r.Context(), x)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
		handler.ServeHTTP(w, r)
	})
}
