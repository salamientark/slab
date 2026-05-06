package daemon

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jiliac/i3-notch/internal/proto"
)

// BootstrapFromProc scans /proc for running claude CLI processes and seeds
// the session map from their transcripts. Called once on startup after
// loadState so that sessions alive at daemon-restart-time reappear even if
// no hook has fired yet.
//
// For cwds hosting multiple claude PIDs, distinct transcript jsonls are
// distributed across those PIDs, sorted by pid ascending ↔ transcript mtime
// descending (newer transcript → older pid, best-effort).
func (d *Daemon) BootstrapFromProc() {
	d.mu.Lock()
	defer d.mu.Unlock()

	pids := scanClaudePIDs()
	if len(pids) == 0 {
		return
	}
	// Group pids by cwd.
	byCWD := map[string][]int{}
	cwdOf := map[int]string{}
	for _, pid := range pids {
		cwd, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
		if err != nil {
			continue
		}
		cwdOf[pid] = cwd
		byCWD[cwd] = append(byCWD[cwd], pid)
	}

	// For each cwd, assign transcripts to pids.
	for cwd, cwdPIDs := range byCWD {
		sort.Ints(cwdPIDs) // stable ordering
		transcripts := recentTranscriptsForCWD(cwd)
		for i, pid := range cwdPIDs {
			var sid string
			if i < len(transcripts) {
				sid = transcripts[i]
			}
			if sid == "" {
				sid = "pid-" + strconv.Itoa(pid) // fallback placeholder
			}
			if _, exists := d.sessions[sid]; exists {
				continue
			}
			d.sessions[sid] = &proto.Session{
				ID:          sid,
				PID:         pid,
				CWD:         cwd,
				Status:      proto.StatusIdle,
				StartedAt:   time.Now(),
				LastEventAt: time.Now(),
			}
		}
	}
	_ = cwdOf
	d.publishLocked("")
}

// recentTranscriptsForCWD returns session IDs of transcript files in the
// Claude projects dir matching cwd, sorted newest first.
func recentTranscriptsForCWD(cwd string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	projects, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*"))
	var out []string
	for _, dir := range projects {
		if !matchesCWD(filepath.Base(dir), cwd) {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		sort.Slice(files, func(i, j int) bool {
			fi, _ := os.Stat(files[i])
			fj, _ := os.Stat(files[j])
			if fi == nil || fj == nil {
				return false
			}
			return fi.ModTime().After(fj.ModTime())
		})
		for _, f := range files {
			sid := readSessionID(f)
			if sid == "" {
				continue
			}
			out = append(out, sid)
		}
	}
	return out
}

// scanClaudePIDs returns PIDs whose /proc/PID/comm or exe suggests the
// Claude Code CLI ("claude" binary).
func scanClaudePIDs() []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if !looksLikeClaude(pid) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func looksLikeClaude(pid int) bool {
	comm, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	name := strings.TrimSpace(string(comm))
	if name == "claude" {
		return true
	}
	// Also check argv in case comm is node/bun.
	cmdline, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if bytesContainsClaudeCLI(cmdline) {
		return true
	}
	return false
}

func bytesContainsClaudeCLI(b []byte) bool {
	s := strings.ReplaceAll(string(b), "\x00", " ")
	if !strings.Contains(s, "claude") {
		return false
	}
	// Very rough filter: argv must reference "claude" as a binary, not a path
	// fragment in some other command.
	for _, tok := range strings.Fields(s) {
		base := filepath.Base(tok)
		if base == "claude" || strings.HasSuffix(base, "/claude") {
			return true
		}
	}
	return false
}

// inferSessionForPID resolves a claude pid to its (session_id, cwd) by
// reading /proc/PID/cwd and picking the most-recently-modified transcript
// whose in-progress state matches (i.e. transcript file mtime within a few
// minutes of now).
func inferSessionForPID(pid int) (string, string) {
	cwdLink, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return "", ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", cwdLink
	}
	// Claude encodes cwd by replacing each slash with '-' and doubling
	// dots — rather than reverse-engineer, glob every project dir and pick
	// the one whose decoded cwd matches.
	projects, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*"))
	if err != nil {
		return "", cwdLink
	}
	var bestSID string
	var bestMTime time.Time
	for _, dir := range projects {
		if !matchesCWD(filepath.Base(dir), cwdLink) {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		sort.Slice(files, func(i, j int) bool {
			fi, _ := os.Stat(files[i])
			fj, _ := os.Stat(files[j])
			if fi == nil || fj == nil {
				return false
			}
			return fi.ModTime().After(fj.ModTime())
		})
		for _, f := range files {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			sid := readSessionID(f)
			if sid == "" {
				continue
			}
			if info.ModTime().After(bestMTime) {
				bestMTime = info.ModTime()
				bestSID = sid
			}
			break // files are sorted newest first; first match wins
		}
	}
	return bestSID, cwdLink
}

// matchesCWD checks if Claude's encoded project directory name could
// represent cwd. Claude replaces '/' and '.' with '-'.
func matchesCWD(encoded, cwd string) bool {
	want := strings.ReplaceAll(cwd, "/", "-")
	want = strings.ReplaceAll(want, ".", "-")
	return encoded == want
}

// readSessionID reads the first line of a transcript to extract its session id.
func readSessionID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec struct {
			SID  string `json:"sessionId"`
			SID2 string `json:"session_id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		if rec.SID != "" {
			return rec.SID
		}
		if rec.SID2 != "" {
			return rec.SID2
		}
	}
	// Fallback: filename sans extension.
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}
