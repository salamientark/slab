// Package proto defines the line-delimited JSON protocol between
// hook scripts, the daemon, and subscribers.
package proto

import "time"

// Status is the derived state of a single Claude Code session.
type Status string

const (
	StatusIdle     Status = "idle"
	StatusRunning  Status = "running"
	StatusAwaiting Status = "awaiting"
	StatusEnded    Status = "ended"
)

// Event is a message produced by a hook script and sent to the daemon.
// Every hook invocation sends exactly one Event on the unix socket.
type Event struct {
	Kind      string    `json:"kind"`                // SessionStart, PreToolUse, PermissionRequest, PostToolUse, Stop, UserPromptSubmit, SessionEnd
	SessionID string    `json:"session_id"`          // Claude session UUID
	PID       int       `json:"pid,omitempty"`       // parent pid of hook (= claude CLI)
	CWD       string    `json:"cwd,omitempty"`       // working directory
	TTY       string    `json:"tty,omitempty"`       // /dev/pts/N if available
	ToolName  string    `json:"tool_name,omitempty"` // for PreToolUse/PostToolUse/PermissionRequest
	ToolUseID string    `json:"tool_use_id,omitempty"`
	TmuxSess  string    `json:"tmux_session,omitempty"`
	TmuxWin   string    `json:"tmux_window,omitempty"`
	TmuxPane  string    `json:"tmux_pane,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	// RequestID is used for events that expect a response (PermissionRequest).
	RequestID string `json:"request_id,omitempty"`
}

// Decision is the daemon's response to a blocking event (PermissionRequest).
type Decision struct {
	RequestID string `json:"request_id"`
	Allow     bool   `json:"allow"`
	Reason    string `json:"reason,omitempty"`
}

// Session is the daemon-side state record for one Claude session.
type Session struct {
	ID          string    `json:"id"`
	PID         int       `json:"pid"`
	CWD         string    `json:"cwd"`
	TTY         string    `json:"tty"`
	TmuxSess    string    `json:"tmux_session,omitempty"`
	TmuxWin     string    `json:"tmux_window,omitempty"`
	TmuxPane    string    `json:"tmux_pane,omitempty"`
	Status      Status    `json:"status"`
	LastTool    string    `json:"last_tool,omitempty"`
	LastEventAt time.Time `json:"last_event_at"`
	StartedAt   time.Time `json:"started_at"`
}

// Snapshot is what subscribers receive: the aggregate view plus the
// triggering session id.
type Snapshot struct {
	TriggerSession string              `json:"trigger_session,omitempty"`
	Sessions       map[string]*Session `json:"sessions"`
	AggStatus      Status              `json:"agg_status"` // worst-of
	AggCount       int                 `json:"agg_count"`
}
