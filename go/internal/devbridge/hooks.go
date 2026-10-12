package devbridge

import (
	"encoding/json"
)

// NormalizeHook receives local input only and returns four safe fields. The
// trusted host supplies an opaque task id and sequence; transcript/session ids,
// messages, tool arguments, paths and output never become task/event fields.
// Hook contracts: Claude Code 2.1.263 stdin events and Codex 0.153.4 legacy notify.
func NormalizeHook(provider string, raw []byte, taskID string, sequence int64) (Task, error) {
	if len(raw) > 64*1024 || !opaque(taskID) || sequence < 1 || sequence > 9007199254740991 {
		return Task{}, ErrRejected
	}
	var input struct {
		Hook       string `json:"hook_event_name"`
		Type       string `json:"type"`
		StopActive bool   `json:"stop_hook_active"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return Task{}, ErrRejected
	}
	state := ""
	switch provider {
	case "claude":
		switch input.Hook {
		case "UserPromptSubmit":
			state = "started"
		case "PostToolUse":
			state = "running"
		case "Stop":
			if !input.StopActive {
				state = "completed"
			}
		case "StopFailure":
			state = "failed"
		}
	case "codex":
		if input.Type == "agent-turn-complete" {
			state = "completed"
		}
	}
	if state == "" {
		return Task{}, ErrRejected
	}
	return Task{Provider: provider, TaskID: taskID, Sequence: sequence, State: state}, nil
}
