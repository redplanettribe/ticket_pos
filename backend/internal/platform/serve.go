package platform

import (
	"context"
	"net"
	"net/http"
	"time"
)

// shutdownDrainTimeout bounds how long a graceful shutdown waits for requests
// in flight. With ShutdownFlushTimeout after it, the whole shutdown fits inside
// the ten seconds Cloud Run allows between SIGTERM and SIGKILL.
const shutdownDrainTimeout = 7 * time.Second

// Serve answers on listener until ctx ends, then shuts down gracefully: it
// stops accepting, lets requests in flight finish within a bound, and flushes
// the reporter with ShutdownFlushTimeout before it returns.
//
// It returns the error that stopped the server, or nil after a shutdown ctx
// asked for, even one that cut off requests at the bound. A server that stops
// on its own is not flushed here: the caller reports the error with
// ReportStartupFailure, which flushes.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, reporter Reporter, logger Logger) error {
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	select {
	case err := <-served:
		// The caller reports this, which flushes it.
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	drainCtx, cancel := context.WithTimeout(context.Background(), shutdownDrainTimeout)
	defer cancel()
	if err := server.Shutdown(drainCtx); err != nil {
		// Requests still in flight at the bound are cut off; the shutdown
		// itself is what was asked for, so it is not a failure to report.
		logger.Warn("requests still in flight at shutdown", "error", err)
	}
	<-served
	if !reporter.Flush(ShutdownFlushTimeout) {
		logger.Warn("error reports not flushed before shutdown", "timeout", ShutdownFlushTimeout.String())
	}
	return nil
}
