// Command notchd is the i3-notch state daemon.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jiliac/i3-notch/internal/daemon"
)

func main() {
	socket := defaultSocketPath()
	if s := os.Getenv("I3_NOTCH_SOCKET"); s != "" {
		socket = s
	}

	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	state := defaultStatePath()
	if s := os.Getenv("I3_NOTCH_STATE"); s != "" {
		state = s
	}
	if err := os.MkdirAll(filepath.Dir(state), 0o700); err != nil {
		log.Fatalf("mkdir state: %v", err)
	}

	d := daemon.New(socket, state)

	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Println("shutdown")
		cancel()
	}()

	if err := d.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func defaultSocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "i3-notch.sock")
	}
	return fmt.Sprintf("/run/user/%d/i3-notch.sock", os.Getuid())
}

func defaultStatePath() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "i3-notch", "state.json")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "i3-notch", "state.json")
	}
	return "/tmp/i3-notch.state.json"
}
