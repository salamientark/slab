package daemon

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/jiliac/i3-notch/internal/proto"
)

// askViaDunst fires dunstify with two actions and returns the user's
// choice. Returns ("allow", true) on accept, ("deny", false) on deny,
// and ("", false) on timeout/error (caller should fall back).
func askViaDunst(ev proto.Event) (proto.Decision, error) {
	title := "Claude Code"
	body := fmt.Sprintf("Allow <b>%s</b>?", html(ev.ToolName))
	if ev.CWD != "" {
		body += fmt.Sprintf("\n<small>%s</small>", html(ev.CWD))
	}

	// dunstify -A <key>,<label>  prints <key> on click, or empty on timeout.
	cmd := exec.Command("dunstify",
		"--urgency=critical",
		"--appname=i3-notch",
		"-A", "allow,Allow",
		"-A", "deny,Deny",
		"--timeout=25000",
		title, body,
	)
	out, err := cmd.Output()
	choice := strings.TrimSpace(string(out))

	dec := proto.Decision{RequestID: ev.RequestID}
	switch choice {
	case "allow":
		dec.Allow = true
	case "deny":
		dec.Reason = "user denied via notification"
	default:
		dec.Reason = "no response from notification"
	}
	return dec, err
}

// html escapes the minimum set dunstify treats as markup.
func html(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
