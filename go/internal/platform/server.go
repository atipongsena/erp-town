package platform

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const shutdownGrace = 10 * time.Second

// NewMux returns the base HTTP mux with the health endpoint registered.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /town/api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

// Serve runs h on addr until ctx is cancelled, then shuts down with a 10 s grace.
func Serve(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
