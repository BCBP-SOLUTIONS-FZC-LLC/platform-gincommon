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

	// Consumers copy this server: always bound header/body reads and keep-alive idling
	// so slow or idle clients (slowloris) cannot pin connections and goroutines.
	// WriteTimeout is left unset: it would cut off slow-but-legitimate responses and is
	// better enforced per request with gincommon.Config.RequestTimeout, which the demo
	// router sets to server.DemoRequestTimeout.
	srv := &http.Server{
		Addr:              config.AppAddr(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
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
