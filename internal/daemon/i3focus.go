package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// i3WindowEvent is the subset of the i3 IPC window-event we care about.
type i3WindowEvent struct {
	Change    string `json:"change"`
	Container struct {
		Window uint32 `json:"window"`
	} `json:"container"`
}

// watchI3Focus subscribes to i3 window events and clears Notify on the
// session whose terminal just got focused. Reconnects on disconnect.
func (d *Daemon) watchI3Focus(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, "i3-msg", "-t", "subscribe", "-m", `["window"]`)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("i3 subscribe pipe: %v", err)
			if !sleepOrDone(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if err := cmd.Start(); err != nil {
			log.Printf("i3 subscribe start: %v", err)
			if !sleepOrDone(ctx, 2*time.Second) {
				return
			}
			continue
		}
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var ev i3WindowEvent
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
				continue
			}
			if ev.Change != "focus" || ev.Container.Window == 0 {
				continue
			}
			focusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			d.handleFocus(focusCtx, uint64(ev.Container.Window))
			cancel()
		}
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return
		}
		log.Printf("i3 window subscription ended; retrying")
		if !sleepOrDone(ctx, time.Second) {
			return
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (d *Daemon) handleFocus(ctx context.Context, xwin uint64) {
	d.mu.Lock()
	type cand struct {
		sid string
		pid int
	}
	var notifyCands []cand
	for id, s := range d.sessions {
		if s.Notify && s.PID > 0 {
			notifyCands = append(notifyCands, cand{id, s.PID})
		}
	}
	d.mu.Unlock()

	for _, c := range notifyCands {
		if sessionOwnsWindow(ctx, c.pid, xwin) {
			d.mu.Lock()
			if s, ok := d.sessions[c.sid]; ok && s.Notify {
				s.Notify = false
				d.publishLocked(c.sid)
			}
			d.mu.Unlock()
			return
		}
	}
}

// sessionOwnsWindow walks up pid's parent chain (max 8 hops) and asks
// xdotool whether any ancestor owns xwin.
func sessionOwnsWindow(ctx context.Context, pid int, xwin uint64) bool {
	cur := pid
	for hop := 0; hop < 8 && cur > 1; hop++ {
		for _, w := range xdotoolSearchAll(ctx, cur) {
			if w == xwin {
				return true
			}
		}
		ppid, err := parentPID(cur)
		if err != nil || ppid == cur {
			break
		}
		cur = ppid
	}
	return false
}

func xdotoolSearchAll(ctx context.Context, pid int) []uint64 {
	out, err := exec.CommandContext(ctx, "xdotool", "search", "--all", "--pid", strconv.Itoa(pid)).Output()
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

func parentPID(pid int) (int, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	s := string(b)
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
