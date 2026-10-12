package agents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPUsesSameRegistryPolicyAndDurableCallIdentity(t *testing.T) {
	x, p, l := policyFixture(t)
	server, err := NewMCPServer(t.Context(), x)
	require.NoError(t, err)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "local-fixture", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()
	list, err := cs.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, list.Tools, 8)
	expected := map[string]ToolDefinition{}
	for _, tool := range x.Tools() {
		expected[tool.Name] = tool
	}
	for _, tool := range list.Tools {
		definition, ok := expected[tool.Name]
		require.True(t, ok)
		schema, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		assert.JSONEq(t, string(definition.InputSchema), string(schema))
	}
	args := json.RawMessage(draftArgs)
	params := &mcp.CallToolParams{Name: "create_sales_order_draft", Arguments: args, Meta: mcp.Meta{MCPCallIDKey: "CALL-1"}}
	first, err := cs.CallTool(t.Context(), params)
	require.NoError(t, err)
	assert.False(t, first.IsError)
	second, err := cs.CallTool(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, first.Content[0].(*mcp.TextContent).Text, second.Content[0].(*mcp.TextContent).Text)
	assert.Len(t, l.calls, 1)
	p.enabled = false
	revoked, err := cs.CallTool(t.Context(), params)
	require.NoError(t, err)
	assert.True(t, revoked.IsError)
}

func TestMCPRequiresStableCallIDBeforeEffect(t *testing.T) {
	x, _, l := policyFixture(t)
	server, err := NewMCPServer(t.Context(), x)
	require.NoError(t, err)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), st, nil)
	require.NoError(t, err)
	defer func() { _ = ss.Close() }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()
	result, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "create_sales_order_draft", Arguments: json.RawMessage(draftArgs)})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Empty(t, l.calls)
}

func TestMCPHTTPRequiresTrustedAuthenticationAndRejectsBrowserOrigin(t *testing.T) {
	called := false
	handler := NewMCPHandler(func(*http.Request) (*PolicyExecutor, error) { called = true; return nil, ErrInvalidRun })
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.True(t, called)
	called = false
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, called)
}
