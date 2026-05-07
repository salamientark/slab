package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jiliac/i3-notch/internal/proto"
)

const dunstTotalTimeout = 25 * time.Second

// askViaDunst fires dunstify with three actions (focus/allow/deny). Body
// click ("default") focuses the session and re-prompts. Allow/Deny resolve
// the request. Returns an empty-reason decision on overall timeout.
func askViaDunst(ctx context.Context, ev proto.Event) (proto.Decision, error) {
	title := "Claude Code"
	body := fmt.Sprintf("Allow <b>%s</b>?", htmlEscape(ev.ToolName))
	if ev.CWD != "" {
		body += fmt.Sprintf("\n<small>%s</small>", htmlEscape(ev.CWD))
	}

	deadline := time.Now().Add(dunstTotalTimeout)
	var lastErr error

	for {
		remaining := time.Until(deadline)
		if remaining <= 500*time.Millisecond {
			break
		}
		ms := int(remaining / time.Millisecond)

		cmd := exec.CommandContext(ctx, "dunstify",
			"--urgency=critical",
			"--appname=i3-notch",
			"-A", "default,Focus",
			"-A", "allow,Allow",
			"-A", "deny,Deny",
			fmt.Sprintf("--timeout=%d", ms),
			title, body,
		)
		out, err := cmd.Output()
		lastErr = err
		choice := strings.TrimSpace(string(out))

		dec := proto.Decision{RequestID: ev.RequestID}
		switch choice {
		case "default":
			focusSession(ctx, ev.SessionID)
			continue
		case "allow":
			dec.Allow = true
			return dec, err
		case "deny":
			dec.Reason = "user denied via notification"
			return dec, err
		default:
			// Empty stdout: either dunstify hit --timeout=remaining (overall
			// deadline reached — outer guard breaks next iter) or it exited
			// early (dunst restarted, dunstctl close-all, etc.). If real time
			// remains, re-prompt instead of silently denying.
			if time.Until(deadline) > 500*time.Millisecond {
				if !sleepOrDone(ctx, time.Second) {
					dec.Reason = "shutdown"
					return dec, ctx.Err()
				}
				continue
			}
			dec.Reason = "no response from notification"
			return dec, err
		}
	}
	return proto.Decision{RequestID: ev.RequestID, Reason: "no response from notification"}, lastErr
}

// focusSession spawns `notchctl jump SID` to bring the session window to
// front. Best-effort; errors are ignored — the dunst loop keeps prompting.
func focusSession(ctx context.Context, sid string) {
	bin := "notchctl"
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "notchctl")
		if _, err := os.Stat(candidate); err == nil {
			bin = candidate
		}
	}
	go func() {
		_ = exec.CommandContext(ctx, bin, "jump", sid).Run()
	}()
}

// htmlEscape escapes the minimum set dunstify treats as markup.
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
