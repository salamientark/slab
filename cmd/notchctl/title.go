package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// sessionTitle returns a short human-readable title for a session, derived
// from the first real user prompt in its JSONL transcript. Results are cached
// per session id until the transcript file's mtime advances.
func sessionTitle(sessionID string) string {
	return titleCache.get(sessionID)
}

type cachedTitle struct {
	title string
	mtime time.Time
	path  string
}

type titleStore struct {
	mu sync.Mutex
	m  map[string]cachedTitle
}

var titleCache = &titleStore{m: map[string]cachedTitle{}}

func (t *titleStore) get(sid string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.m[sid]
	path := entry.path
	if path == "" {
		matches, _ := filepath.Glob(homeDir() + "/.claude/projects/*/" + sid + ".jsonl")
		if len(matches) == 0 {
			return ""
		}
		path = matches[0]
	}

	fi, err := os.Stat(path)
	if err != nil {
		return entry.title
	}
	if ok && fi.ModTime().Equal(entry.mtime) && entry.title != "" {
		return entry.title
	}

	title := extractFirstUserPrompt(path)
	t.m[sid] = cachedTitle{title: title, mtime: fi.ModTime(), path: path}
	return title
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

var stripTagRE = regexp.MustCompile(`<[^>]{1,60}>`)

// extractFirstUserPrompt scans the transcript for the first user message
// that isn't a local-command-caveat / command-name wrapper, and returns a
// trimmed first-line excerpt.
func extractFirstUserPrompt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Type != "user" || rec.Message.Role != "user" {
			continue
		}
		raw := contentToText(rec.Message.Content)
		if isCommandMessage(raw) {
			continue
		}
		text := cleanPromptText(raw)
		if text == "" || strings.HasPrefix(text, "/") {
			continue
		}
		return text
	}
	return ""
}

// isCommandMessage returns true for slash-command wrappers and local-command
// stdout / caveat blocks — anything not authored directly by the user.
func isCommandMessage(s string) bool {
	if strings.Contains(s, "<command-name>") {
		return true
	}
	if strings.Contains(s, "<local-command-stdout>") {
		return true
	}
	if strings.Contains(s, "<local-command-caveat>") {
		return true
	}
	return false
}

func contentToText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "text" {
					if s, _ := m["text"].(string); s != "" {
						parts = append(parts, s)
					}
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func cleanPromptText(s string) string {
	// Strip XML-ish tags: <local-command-caveat>, <command-name>, <system-reminder>, etc.
	s = stripTagRE.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "\r", " ")
	// First non-empty line.
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip if line looks like residual command boilerplate.
		if strings.HasPrefix(line, "Caveat:") || strings.HasPrefix(line, "DO NOT respond") {
			continue
		}
		return line
	}
	return ""
}
