// Command notchctl is the client CLI for notchd.
//
// Subcommands:
//
//	notchctl publish       # read one or more Event JSONs from stdin, forward to daemon
//	notchctl subscribe     # emit polybar-format status lines on each state change
//	notchctl jump [SID]    # focus the terminal for the highest-priority session (or SID)
//	notchctl decide ID Y/N # respond to a pending PermissionRequest
//	notchctl list          # dump current state as JSON
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jiliac/i3-notch/internal/proto"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "publish":
		err = runPublish()
	case "subscribe":
		err = runSubscribe(args)
	case "jump":
		err = runJump(args)
	case "decide":
		err = runDecide(args)
	case "list":
		err = runList()
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  notchctl publish             read Event JSONs from stdin
  notchctl subscribe           stream polybar-format lines
  notchctl jump [SESSION_ID]   focus session terminal
  notchctl decide ID allow|deny
  notchctl list
`)
}

func socketPath() string {
	if s := os.Getenv("I3_NOTCH_SOCKET"); s != "" {
		return s
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "i3-notch.sock")
	}
	return fmt.Sprintf("/run/user/%d/i3-notch.sock", os.Getuid())
}

func dial() (net.Conn, error) {
	return net.Dial("unix", socketPath())
}

// runPublish forwards each line of stdin as an Event.
func runPublish() error {
	conn, err := dial()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, `{"role":"publish"}`); err != nil {
		return err
	}
	rd := bufio.NewReader(os.Stdin)
	respRd := bufio.NewReader(conn)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 {
			if _, err := conn.Write(line); err != nil {
				return err
			}
			resp, rerr := respRd.ReadBytes('\n')
			if rerr != nil {
				return rerr
			}
			// Forward daemon response to stdout so hooks can capture Decision JSON.
			os.Stdout.Write(resp)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func runList() error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, `{"role":"command","cmd":"list"}`); err != nil {
		return err
	}
	_, err = io.Copy(os.Stdout, conn)
	return err
}

func runDecide(args []string) error {
	if len(args) != 2 {
		return errors.New("decide: need REQUEST_ID allow|deny")
	}
	allow := strings.EqualFold(args[1], "allow") || args[1] == "y" || args[1] == "yes"
	dec := proto.Decision{RequestID: args[0], Allow: allow}
	if !allow {
		dec.Reason = "user denied"
	}
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, `{"role":"decide"}`); err != nil {
		return err
	}
	b, _ := json.Marshal(dec)
	_, err = conn.Write(append(b, '\n'))
	return err
}

func runSubscribe(args []string) error {
	format := "polybar"
	for i, a := range args {
		if a == "--format" && i+1 < len(args) {
			format = args[i+1]
		}
	}
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, `{"role":"subscribe"}`); err != nil {
		return err
	}
	dec := json.NewDecoder(conn)
	for {
		var snap proto.Snapshot
		if err := dec.Decode(&snap); err != nil {
			return err
		}
		switch format {
		case "polybar":
			fmt.Println(formatPolybar(snap))
		case "json":
			b, _ := json.Marshal(snap)
			fmt.Println(string(b))
		default:
			fmt.Println(formatPlain(snap))
		}
	}
}

func formatPlain(s proto.Snapshot) string {
	return fmt.Sprintf("%s (%d)", s.AggStatus, s.AggCount)
}

// runJump focuses the terminal for a session. If no SID given, picks the
// session with the worst status (awaiting > running > idle).
func runJump(args []string) error {
	snap, err := fetchSnapshot()
	if err != nil {
		return err
	}
	var target *proto.Session
	if len(args) > 0 {
		target = snap.Sessions[args[0]]
	} else {
		target = pickNotifyOrWorst(snap)
	}
	if target == nil {
		return errors.New("no session to jump to")
	}
	if err := focusSession(target); err != nil {
		return err
	}
	_ = ackSession(target.ID)
	return nil
}

func ackSession(sid string) error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	msg := fmt.Sprintf(`{"role":"command","cmd":"ack","sid":%q}`+"\n", sid)
	if _, err := conn.Write([]byte(msg)); err != nil {
		return err
	}
	return nil
}

func fetchSnapshot() (*proto.Snapshot, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := fmt.Fprintln(conn, `{"role":"command","cmd":"list"}`); err != nil {
		return nil, err
	}
	var snap proto.Snapshot
	if err := json.NewDecoder(conn).Decode(&snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func pickWorst(s *proto.Snapshot) *proto.Session {
	var best *proto.Session
	bestRank := -1
	for _, sess := range s.Sessions {
		r := statusRank(sess.Status)
		if r > bestRank {
			bestRank = r
			best = sess
		}
	}
	return best
}

// pickNotifyOrWorst prefers an unacknowledged-completed session, else worst.
func pickNotifyOrWorst(s *proto.Snapshot) *proto.Session {
	for _, sess := range s.Sessions {
		if sess.Notify {
			return sess
		}
	}
	return pickWorst(s)
}

func statusRank(s proto.Status) int {
	switch s {
	case proto.StatusAwaiting:
		return 3
	case proto.StatusRunning:
		return 2
	case proto.StatusIdle:
		return 1
	}
	return 0
}

// focusSession resolves the session's pid to an X11 window, then asks i3
// to focus it. Also replays tmux select-pane if tmux info was captured.
func focusSession(s *proto.Session) error {
	if s.PID == 0 {
		return errors.New("session has no pid")
	}
	xwin, err := xdotoolFindWindow(s.PID)
	if err != nil {
		return err
	}
	conID, err := i3ConIDForWindow(xwin)
	if err != nil {
		return err
	}
	if err := run("i3-msg", fmt.Sprintf(`[con_id=%d] focus`, conID)); err != nil {
		return err
	}
	if s.TmuxSess != "" && s.TmuxWin != "" && s.TmuxPane != "" {
		_ = run("tmux", "select-window", "-t", s.TmuxSess+":"+s.TmuxWin)
		_ = run("tmux", "select-pane", "-t", s.TmuxSess+":"+s.TmuxWin+"."+s.TmuxPane)
	}
	return nil
}

// xdotoolFindWindow walks up the process tree until a pid owns an X11 window
// that is present in the i3 tree (i.e. a real client window, not a notification
// or other transient).
func xdotoolFindWindow(pid int) (uint64, error) {
	tree, err := getI3Tree()
	if err != nil {
		return 0, err
	}
	wsMap := buildWindowWorkspaceMap(tree)

	cur := pid
	for hop := 0; hop < 8 && cur > 1; hop++ {
		for _, xw := range xdotoolSearchAll(cur) {
			if _, ok := wsMap[xw]; ok {
				return xw, nil
			}
		}
		ppid, perr := parentPID(cur)
		if perr != nil || ppid == cur {
			break
		}
		cur = ppid
	}
	return 0, fmt.Errorf("no window found for pid %d (walked parents)", pid)
}

func i3ConIDForWindow(xwin uint64) (uint64, error) {
	out, err := exec.Command("i3-msg", "-t", "get_tree").Output()
	if err != nil {
		return 0, fmt.Errorf("i3-msg get_tree: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		return 0, err
	}
	if id := walkForWindow(root, xwin); id != 0 {
		return id, nil
	}
	return 0, fmt.Errorf("window 0x%x not found in i3 tree", xwin)
}

func walkForWindow(node map[string]any, xwin uint64) uint64 {
	if w, ok := node["window"].(float64); ok && uint64(w) == xwin {
		if id, ok := node["id"].(float64); ok {
			return uint64(id)
		}
	}
	for _, key := range []string{"nodes", "floating_nodes"} {
		if kids, ok := node[key].([]any); ok {
			for _, k := range kids {
				if child, ok := k.(map[string]any); ok {
					if id := walkForWindow(child, xwin); id != 0 {
						return id
					}
				}
			}
		}
	}
	return 0
}

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stderr = os.Stderr
	return c.Run()
}
