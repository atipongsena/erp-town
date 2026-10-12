package devbridge

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHookNormalizationDropsPrivateFieldsAndUnsupportedEvents(t *testing.T) {
	for _, tc := range []struct{ provider, raw, state string }{
		{"claude", `{"hook_event_name":"UserPromptSubmit","prompt":"secret","cwd":"private/path","session_id":"person"}`, "started"},
		{"claude", `{"hook_event_name":"PostToolUse","tool_input":{"command":"private code"}}`, "running"},
		{"claude", `{"hook_event_name":"Stop","last_assistant_message":"secret","stop_hook_active":false}`, "completed"},
		{"codex", `{"type":"agent-turn-complete","input-messages":["secret"],"last-assistant-message":"secret","cwd":"private/path"}`, "completed"},
	} {
		out, err := NormalizeHook(tc.provider, []byte(tc.raw), "TASK-1", 2)
		require.NoError(t, err)
		require.Equal(t, tc.state, out.State)
		raw, err := json.Marshal(out)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "secret")
		require.NotContains(t, string(raw), "private")
		require.NotContains(t, string(raw), "person")
	}
	for _, raw := range []string{`{"type":"approval-requested"}`, `{"type":"turn-started"}`, `{"type":"agent-turn-complete","cwd":"x"} {}`} {
		_, err := NormalizeHook("codex", []byte(raw), "TASK-1", 1)
		require.Error(t, err)
	}
	_, err := NormalizeHook("claude", []byte(`{"hook_event_name":"Stop","stop_hook_active":true}`), "TASK-1", 1)
	require.Error(t, err)
}
