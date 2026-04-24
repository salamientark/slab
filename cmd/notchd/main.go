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

	d := daemon.New(socket)

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
