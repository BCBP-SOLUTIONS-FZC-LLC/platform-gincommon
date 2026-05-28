package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/config"
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/internal/server"
)

func main() {
	router, shutdownTracing, err := server.Setup()
	if err != nil {
		panic(err)
	}
	// shutdownTracing is called explicitly in every exit path below so that
	// os.Exit (used in error paths) does not skip the OTel span flush.
	// log.Fatal / log.Fatalf call os.Exit(1) which bypasses defer, so we
	// never use log.Fatal here.

	srv := &http.Server{
		Addr:    config.AppAddr(),
		Handler: router,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	listenErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
	}()

	select {
	case err := <-listenErr:
		shutdownTracing() // flush spans before exiting
		log.Printf("listen: %v", err)
		os.Exit(1)
	case <-quit:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		shutdownTracing() // flush spans before exiting
		log.Printf("server forced shutdown: %v", err)
		os.Exit(1)
	}
	shutdownTracing() // normal graceful shutdown path
}
