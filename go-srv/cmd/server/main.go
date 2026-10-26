// Command server runs the go-srv HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HectorVeraO/initializer/go-srv/internal/gatbase/health"
)

const (
	defaultAddr       = ":10100"
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	err := run(logger)
	if err != nil {
		logger.Error("server_failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// run serves HTTP until SIGINT or SIGTERM, then drains in-flight requests.
func run(logger *slog.Logger) error {
	addr := os.Getenv("GO_SRV_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	mux := http.NewServeMux()
	health.Register(mux)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: readHeaderTimeout}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server_starting", slog.String("addr", addr))
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("server_stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	err := server.Shutdown(shutdownCtx)
	if err != nil {
		return err
	}
	err = <-serveErr
	isCleanClose := errors.Is(err, http.ErrServerClosed)
	if !isCleanClose {
		return err
	}
	return nil
}
