package health

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type WriteHealthChecker interface {
	WriteHealthy(threshold time.Duration) bool
}

func Serve(port int, checker WriteHealthChecker) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleLiveness)
	mux.HandleFunc("/readyz", handleReadiness(checker))

	addr := fmt.Sprintf(":%d", port)
	slog.Info("starting health server", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("health server exited", "error", err)
	}
}

func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handleReadiness(checker WriteHealthChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if checker.WriteHealthy(10 * time.Minute) {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}
}
