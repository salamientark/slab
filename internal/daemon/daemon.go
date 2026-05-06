// Package daemon implements notchd: a single-writer state store and
// pub/sub fan-out over a unix socket.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/jiliac/i3-notch/internal/proto"
)

const (
	livenessProbeInterval = 2 * time.Second
	approvalTimeout       = 30 * time.Second
	staleRunningTTL       = 5 * time.Minute
	staleAwaitingTTL      = 10 * time.Minute
)

// Daemon owns all session state and connected subscribers.
type Daemon struct {
	socketPath string
	statePath  string

	mu       sync.Mutex
	sessions map[string]*proto.Session
	subs     map[chan proto.Snapshot]struct{}

	// Pending approval requests: request_id -> response channel.
	pending map[string]chan proto.Decision
}

// New constructs a Daemon bound to socketPath. statePath, if non-empty, is
// where sessions are persisted across restarts.
func New(socketPath, statePath string) *Daemon {
	d := &Daemon{
		socketPath: socketPath,
		statePath:  statePath,
		sessions:   make(map[string]*proto.Session),
		subs:       make(map[chan proto.Snapshot]struct{}),
		pending:    make(map[string]chan proto.Decision),
	}
	d.loadState()
	return d
}

func (d *Daemon) loadState() {
	if d.statePath == "" {
		return
	}
	b, err := os.ReadFile(d.statePath)
	if err != nil {
		return
	}
	var loaded map[string]*proto.Session
	if err := json.Unmarshal(b, &loaded); err != nil {
		log.Printf("loadState: %v", err)
		return
	}
	// Filter dead pids on load.
	for id, s := range loaded {
		if s.PID > 0 && syscall.Kill(s.PID, 0) == nil {
			d.sessions[id] = s
		}
	}
	log.Printf("loaded %d sessions from %s", len(d.sessions), d.statePath)
}

// saveStateLocked persists sessions to disk. Caller must hold d.mu.
func (d *Daemon) saveStateLocked() {
	if d.statePath == "" {
		return
	}
	b, err := json.Marshal(d.sessions)
	if err != nil {
		return
	}
	tmp := d.statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, d.statePath)
}

// Run blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	// Clean up any stale socket.
	_ = os.Remove(d.socketPath)

	l, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", d.socketPath, err)
	}
	defer l.Close()

	if err := os.Chmod(d.socketPath, 0o600); err != nil {
		return fmt.Errorf("chmod socket: %w", err)
	}

	log.Printf("notchd listening on %s", d.socketPath)

	d.BootstrapFromProc()

	go d.livenessProbe(ctx)
	go d.watchI3Focus(ctx)

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("accept: %v", err)
			continue
		}
		go d.handleConn(ctx, conn)
	}
}

// handleConn reads the first line to determine connection role:
//   - {"role":"publish"}  — hook client, sends one or more Events.
//   - {"role":"subscribe"} — subscriber, receives Snapshot stream.
//   - {"role":"decide"}    — decision responder, sends one Decision.
//   - {"role":"command","cmd":"list"}  — one-shot query.
func (d *Daemon) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	rd := bufio.NewReader(conn)
	firstLine, err := rd.ReadBytes('\n')
	if err != nil {
		return
	}

	var header struct {
		Role string `json:"role"`
		Cmd  string `json:"cmd,omitempty"`
		SID  string `json:"sid,omitempty"`
	}
	if err := json.Unmarshal(firstLine, &header); err != nil {
		fmt.Fprintf(conn, `{"error":"bad header: %s"}`+"\n", err)
		return
	}

	switch header.Role {
	case "publish":
		d.servePublisher(rd, conn)
	case "subscribe":
		d.serveSubscriber(ctx, conn)
	case "decide":
		d.serveDecider(rd)
	case "command":
		d.serveCommand(conn, header.Cmd, header.SID)
	default:
		fmt.Fprintf(conn, `{"error":"unknown role %q"}`+"\n", header.Role)
	}
}

func (d *Daemon) servePublisher(rd *bufio.Reader, conn net.Conn) {
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var ev proto.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			fmt.Fprintf(conn, `{"error":"bad event: %s"}`+"\n", err)
			continue
		}

		if ev.Kind == "PermissionRequest" && ev.RequestID != "" {
			// Blocking: wait for decision or timeout.
			decision := d.handleApproval(ev)
			b, _ := json.Marshal(decision)
			conn.Write(append(b, '\n'))
			continue
		}

		d.applyEvent(ev)
		fmt.Fprintln(conn, `{"ok":true}`)
	}
}

func (d *Daemon) serveSubscriber(ctx context.Context, conn net.Conn) {
	ch := make(chan proto.Snapshot, 8)
	d.mu.Lock()
	d.subs[ch] = struct{}{}
	// Send current snapshot immediately.
	initial := d.snapshotLocked("")
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.subs, ch)
		d.mu.Unlock()
	}()

	enc := json.NewEncoder(conn)
	if err := enc.Encode(initial); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case snap := <-ch:
			if err := enc.Encode(snap); err != nil {
				return
			}
		}
	}
}

func (d *Daemon) serveDecider(rd *bufio.Reader) {
	line, err := rd.ReadBytes('\n')
	if err != nil {
		return
	}
	var dec proto.Decision
	if err := json.Unmarshal(line, &dec); err != nil {
		return
	}
	d.mu.Lock()
	ch, ok := d.pending[dec.RequestID]
	if ok {
		delete(d.pending, dec.RequestID)
	}
	d.mu.Unlock()
	if ok {
		select {
		case ch <- dec:
		default:
		}
	}
}

func (d *Daemon) serveCommand(conn net.Conn, cmd, sid string) {
	switch cmd {
	case "list":
		d.mu.Lock()
		snap := d.snapshotLocked("")
		d.mu.Unlock()
		json.NewEncoder(conn).Encode(snap)
	case "ack":
		d.mu.Lock()
		if sid == "" {
			for _, s := range d.sessions {
				s.Notify = false
			}
		} else if s, ok := d.sessions[sid]; ok {
			s.Notify = false
		}
		d.publishLocked(sid)
		d.mu.Unlock()
		fmt.Fprintln(conn, `{"ok":true}`)
	default:
		fmt.Fprintf(conn, `{"error":"unknown cmd %q"}`+"\n", cmd)
	}
}

// handleApproval registers a pending approval request, notifies subscribers
// (they fire dunstify), and blocks until a decide-role client replies.
func (d *Daemon) handleApproval(ev proto.Event) proto.Decision {
	ch := make(chan proto.Decision, 1)

	d.mu.Lock()
	d.pending[ev.RequestID] = ch
	d.mu.Unlock()

	d.applyEvent(ev) // marks session awaiting, publishes snapshot

	// Fire dunstify; deliver its result into the same channel. A concurrent
	// `notchctl decide` (manual override) can still win the race.
	go func() {
		dec, err := askViaDunst(ev)
		if err != nil {
			log.Printf("dunstify: %v", err)
		}
		d.mu.Lock()
		_, stillPending := d.pending[ev.RequestID]
		if stillPending {
			delete(d.pending, ev.RequestID)
		}
		d.mu.Unlock()
		if stillPending {
			select {
			case ch <- dec:
			default:
			}
		}
	}()

	select {
	case dec := <-ch:
		// Transition back to running after decision.
		d.updateStatus(ev.SessionID, proto.StatusRunning)
		return dec
	case <-time.After(approvalTimeout):
		d.mu.Lock()
		delete(d.pending, ev.RequestID)
		d.mu.Unlock()
		d.updateStatus(ev.SessionID, proto.StatusRunning)
		return proto.Decision{RequestID: ev.RequestID, Allow: false, Reason: "timeout"}
	}
}

func (d *Daemon) applyEvent(ev proto.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	s, ok := d.sessions[ev.SessionID]
	if !ok {
		s = &proto.Session{
			ID:        ev.SessionID,
			StartedAt: ev.Timestamp,
		}
		d.sessions[ev.SessionID] = s
	}

	if ev.PID != 0 {
		s.PID = ev.PID
	}
	if ev.CWD != "" {
		s.CWD = ev.CWD
	}
	if ev.TTY != "" {
		s.TTY = ev.TTY
	}
	if ev.TmuxSess != "" {
		s.TmuxSess = ev.TmuxSess
		s.TmuxWin = ev.TmuxWin
		s.TmuxPane = ev.TmuxPane
	}

	switch ev.Kind {
	case "SessionStart":
		s.Status = proto.StatusIdle
	case "UserPromptSubmit":
		s.Notify = false // user engaged with this session
		s.Status = proto.StatusRunning
	case "PreToolUse", "PostToolUse":
		s.Status = proto.StatusRunning
		if ev.ToolName != "" {
			s.LastTool = ev.ToolName
		}
	case "PermissionRequest":
		s.Status = proto.StatusAwaiting
		if ev.ToolName != "" {
			s.LastTool = ev.ToolName
		}
	case "Stop":
		if s.Status == proto.StatusRunning || s.Status == proto.StatusAwaiting {
			s.Notify = true // task finished, mark unseen
		}
		s.Status = proto.StatusIdle
	case "Notification":
		// informational only; do not flip to awaiting (only PermissionRequest does)
	case "SessionEnd":
		s.Status = proto.StatusEnded
	}

	s.LastEventAt = ev.Timestamp
	d.publishLocked(ev.SessionID)
}

func (d *Daemon) updateStatus(sessionID string, st proto.Status) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.sessions[sessionID]; ok {
		s.Status = st
		d.publishLocked(sessionID)
	}
}

func (d *Daemon) publishLocked(trigger string) {
	snap := d.snapshotLocked(trigger)
	for ch := range d.subs {
		select {
		case ch <- snap:
		default:
			// subscriber is slow; drop.
		}
	}
	d.saveStateLocked()
}

// snapshotLocked builds an aggregate view. Caller must hold d.mu.
func (d *Daemon) snapshotLocked(trigger string) proto.Snapshot {
	out := proto.Snapshot{
		TriggerSession: trigger,
		Sessions:       make(map[string]*proto.Session, len(d.sessions)),
	}
	worst := proto.StatusIdle
	for id, s := range d.sessions {
		if s.Status == proto.StatusEnded {
			continue
		}
		// Copy so subscribers never see mutation.
		cp := *s
		out.Sessions[id] = &cp
		out.AggCount++
		if statusRank(s.Status) > statusRank(worst) {
			worst = s.Status
		}
	}
	out.AggStatus = worst
	return out
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

// livenessProbe expires sessions whose pid has gone away.
func (d *Daemon) livenessProbe(ctx context.Context) {
	t := time.NewTicker(livenessProbeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.mu.Lock()
			changed := false
			now := time.Now()
			for id, s := range d.sessions {
				if s.PID == 0 || s.Status == proto.StatusEnded {
					continue
				}
				// 1. dead-pid sweep
				if err := syscall.Kill(s.PID, 0); err != nil {
					s.Status = proto.StatusEnded
					delete(d.sessions, id)
					changed = true
					continue
				}
				// 2. stale-status demotion: alive PID but no events for too long
				age := now.Sub(s.LastEventAt)
				switch s.Status {
				case proto.StatusRunning:
					if age > staleRunningTTL {
						s.Status = proto.StatusIdle
						changed = true
						log.Printf("janitor: %s demoted running → idle (age %s)", id, age.Round(time.Second))
					}
				case proto.StatusAwaiting:
					if age > staleAwaitingTTL {
						s.Status = proto.StatusIdle
						changed = true
						log.Printf("janitor: %s demoted awaiting → idle (age %s)", id, age.Round(time.Second))
					}
				}
			}
			if changed {
				d.publishLocked("")
			}
			d.mu.Unlock()
		}
	}
}
