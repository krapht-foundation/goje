// Command relay pairs two players by room code and forwards their messages
// for online lockstep play. See specs/relay-m16.md for the routes, limits,
// and deployment behind a TLS-terminating reverse proxy.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/krapht-foundation/goje/cmd/relay/relay"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "relay:", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg relay.Config
	addr := flag.String("addr", ":8080", "TCP address to listen on")
	flag.IntVar(&cfg.MaxRooms, "max-rooms", 256, "most rooms open at once")
	flag.Func("origin", "allowed browser Origin; repeat for more (default: any)",
		func(origin string) error {
			cfg.Origins = append(cfg.Origins, origin)
			return nil
		})
	flag.DurationVar(&cfg.Idle, "idle", 30*time.Second, "close a paired room idle this long")
	flag.DurationVar(&cfg.JoinWindow, "join-window", 2*time.Minute,
		"close a room not joined within this time")
	flag.IntVar(&cfg.Rate, "rate", 240, "messages per second allowed on each connection")
	flag.IntVar(&cfg.MaxMessage, "max-message", 2048, "largest client message in bytes")
	flag.Parse()
	cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	rs := relay.New(cfg)
	// The timeouts cover the HTTP exchange before the upgrade; upgraded
	// connections set their own deadlines and are closed by the room timers.
	srv := &http.Server{
		Handler:           rs,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	cfg.Logger.Info("relay listening", "addr", ln.Addr().String())
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case serveErr := <-served:
		return fmt.Errorf("serve: %w", serveErr)
	case <-ctx.Done():
	}
	cfg.Logger.Info("relay shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	rs.Close()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
