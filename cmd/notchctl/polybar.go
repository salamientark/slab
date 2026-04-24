package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jiliac/i3-notch/internal/proto"
)

// chipView is a per-session projection used for rendering.
type chipView struct {
	sid     string
	name    string
	ws      string // workspace name, "" if unknown
	output  string // i3 output (monitor) name
	status  proto.Status
	notify  bool
	tool    string
	project string // project-type glyph
}

const (
	maxChips    = 5
	maxNameLen  = 22
	colorRun    = "#a6e3a1"
	colorAwait  = "#fab387"
	colorIdle   = "#6c7086"
	colorBadge  = "#89b4fa"
	colorMuted  = "#45475a"
	colorBorder = "#313244"
	colorNotify = "#f9e2af" // yellow — "done, please look"
)

var selfExe string // resolved absolute path of this binary

func init() {
	if exe, err := os.Executable(); err == nil {
		selfExe = exe
	} else {
		selfExe = "notchctl"
	}
}

// formatPolybar renders the snapshot as a polybar tail line.
// Chips are sorted: awaiting → running → idle, newest first within tier.
func formatPolybar(s proto.Snapshot) string {
	if s.AggCount == 0 {
		return fmt.Sprintf("%%{F%s}󰚩 idle%%{F-}", colorIdle)
	}

	views := buildChipViews(s)

	var parts []string
	shown := views
	overflow := 0
	if len(views) > maxChips {
		shown = views[:maxChips]
		overflow = len(views) - maxChips
	}
	for _, v := range shown {
		parts = append(parts, renderChip(v))
	}
	if overflow > 0 {
		parts = append(parts, fmt.Sprintf("%%{F%s}+%d%%{F-}", colorMuted, overflow))
	}

	// Global prefix: bot icon colored by aggregate status, clickable to jump worst.
	prefix := fmt.Sprintf("%%{A1:%s jump:}%%{F%s}󰚩%%{F-}%%{A}", selfExe, aggColor(s.AggStatus))
	return prefix + " " + strings.Join(parts, " ")
}

func buildChipViews(s proto.Snapshot) []chipView {
	wsMap := workspaceMapForPIDs(collectPIDs(s))

	out := make([]chipView, 0, len(s.Sessions))
	for id, sess := range s.Sessions {
		cv := chipView{
			sid:     id,
			name:    displayName(id, sess.CWD),
			status:  sess.Status,
			notify:  sess.Notify,
			tool:    sess.LastTool,
			project: projectIcon(sess.CWD),
		}
		if wi, ok := wsMap[sess.PID]; ok {
			cv.ws = wi.ws
			cv.output = wi.output
		}
		out = append(out, cv)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := sortRank(out[i]), sortRank(out[j])
		if ri != rj {
			return ri > rj
		}
		return out[i].name < out[j].name
	})
	return out
}

func sortRank(v chipView) int {
	switch v.status {
	case proto.StatusAwaiting:
		return 4
	case proto.StatusRunning:
		return 3
	case proto.StatusIdle:
		if v.notify {
			return 2 // done-unseen floats above plain idle
		}
		return 1
	}
	return 0
}

func renderChip(v chipView) string {
	var icon, color string
	switch v.status {
	case proto.StatusAwaiting:
		icon = "⚠"
		color = colorAwait
	case proto.StatusRunning:
		icon = "⚡"
		color = colorRun
	case proto.StatusIdle:
		icon = "⏾"
		color = colorIdle
	default:
		icon = "◎"
		color = colorIdle
	}
	if v.notify {
		icon = "✔"
		color = colorNotify
	}
	ws := v.ws
	if ws == "" {
		ws = "?"
	}
	sep := fmt.Sprintf(" %%{F%s}|%%{F-} ", colorMuted)
	nameColor := ""
	nameEnd := ""
	if v.notify {
		nameColor = fmt.Sprintf("%%{F%s}%%{u%s}%%{+u}", colorNotify, colorNotify)
		nameEnd = "%{-u}%{F-}"
	}
	label := fmt.Sprintf("%%{F%s}%s%%{F-}%s%%{F%s}%s%%{F-}%s%s%s%s",
		colorBadge, ws, sep, color, icon, sep, nameColor, v.name, nameEnd)
	click := fmt.Sprintf("%s jump %s", selfExe, v.sid)
	return fmt.Sprintf("%%{A1:%s:}%s%%{A}", click, label)
}

func aggColor(st proto.Status) string {
	switch st {
	case proto.StatusAwaiting:
		return colorAwait
	case proto.StatusRunning:
		return colorRun
	}
	return colorIdle
}

func shortName(cwd string) string {
	base := filepath.Base(cwd)
	if base == "" || base == "/" || base == "." {
		return "~"
	}
	return truncate(base, maxNameLen)
}

// displayName prefers the transcript-derived session title over CWD basename.
func displayName(sid, cwd string) string {
	if t := sessionTitle(sid); t != "" {
		return truncate(t, maxNameLen)
	}
	return shortName(cwd)
}

// projectIcon returns a nerd-font glyph derived from manifest files in cwd.
// Falls back to a folder icon.
func projectIcon(cwd string) string {
	if cwd == "" {
		return ""
	}
	checks := []struct {
		file string
		icon string
	}{
		{"go.mod", ""},       // go
		{"Cargo.toml", ""},   // rust
		{"package.json", ""}, // js/node
		{"pyproject.toml", ""},
		{"requirements.txt", ""},
		{"Gemfile", ""},
		{"pubspec.yaml", ""},
		{"build.gradle", ""},
		{"build.gradle.kts", ""},
		{"pom.xml", ""},
		{"composer.json", ""},
		{"CMakeLists.txt", ""},
		{"Makefile", ""},
		{".git", ""},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(cwd, c.file)); err == nil {
			return c.icon
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---- workspace lookup ---------------------------------------------------

type wsInfo struct {
	ws     string
	output string
}

func collectPIDs(s proto.Snapshot) []int {
	out := make([]int, 0, len(s.Sessions))
	for _, sess := range s.Sessions {
		if sess.PID > 0 {
			out = append(out, sess.PID)
		}
	}
	return out
}

// workspaceMapForPIDs resolves each pid to its (workspace, output) via
// xdotool + i3 tree. Missing pids silently drop out.
func workspaceMapForPIDs(pids []int) map[int]wsInfo {
	res := map[int]wsInfo{}
	if len(pids) == 0 {
		return res
	}
	tree, err := getI3Tree()
	if err != nil {
		return res
	}
	xwinToWS := buildWindowWorkspaceMap(tree)

	for _, pid := range pids {
		if wi, ok := resolvePIDWorkspace(pid, xwinToWS); ok {
			res[pid] = wi
		}
	}
	return res
}

// resolvePIDWorkspace walks up the parent chain until a pid owns an X11
// window registered in xwinToWS. Max 8 hops to avoid loops.
func resolvePIDWorkspace(pid int, xwinToWS map[uint64]wsInfo) (wsInfo, bool) {
	cur := pid
	for hop := 0; hop < 8 && cur > 1; hop++ {
		for _, xw := range xdotoolSearchAll(cur) {
			if wi, ok := xwinToWS[xw]; ok {
				return wi, true
			}
		}
		ppid, err := parentPID(cur)
		if err != nil || ppid == cur {
			break
		}
		cur = ppid
	}
	return wsInfo{}, false
}

func parentPID(pid int) (int, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	s := string(b)
	// Format: pid (comm) state ppid ...
	// comm can contain spaces/parens; split at last ')'.
	rp := strings.LastIndex(s, ")")
	if rp < 0 || rp+1 >= len(s) {
		return 0, fmt.Errorf("bad stat")
	}
	fields := strings.Fields(s[rp+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("bad stat fields")
	}
	return strconv.Atoi(fields[1])
}

func getI3Tree() (map[string]any, error) {
	out, err := exec.Command("i3-msg", "-t", "get_tree").Output()
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		return nil, err
	}
	return root, nil
}

// buildWindowWorkspaceMap walks the i3 tree and maps every X11 window id to
// the workspace (name + output) that contains it.
func buildWindowWorkspaceMap(root map[string]any) map[uint64]wsInfo {
	m := map[uint64]wsInfo{}
	var walk func(node map[string]any, ws, output string)
	walk = func(node map[string]any, ws, output string) {
		if t, _ := node["type"].(string); t == "output" {
			if name, ok := node["name"].(string); ok {
				output = name
			}
		}
		if t, _ := node["type"].(string); t == "workspace" {
			if name, ok := node["name"].(string); ok {
				ws = name
			}
		}
		if w, ok := node["window"].(float64); ok && w != 0 {
			m[uint64(w)] = wsInfo{ws: ws, output: output}
		}
		for _, key := range []string{"nodes", "floating_nodes"} {
			if kids, ok := node[key].([]any); ok {
				for _, k := range kids {
					if child, ok := k.(map[string]any); ok {
						walk(child, ws, output)
					}
				}
			}
		}
	}
	walk(root, "", "")
	return m
}

func xdotoolSearchAll(pid int) []uint64 {
	out, err := exec.Command("xdotool", "search", "--all", "--pid", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil
	}
	var res []uint64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if id, err := strconv.ParseUint(strings.TrimSpace(line), 10, 64); err == nil {
			res = append(res, id)
		}
	}
	return res
}
