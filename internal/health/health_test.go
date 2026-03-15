package health_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/home-operations/newt-sidecar/internal/health"
)

// stubChecker implements WriteHealthChecker for testing.
type stubChecker struct {
	healthy bool
}

func (s *stubChecker) WriteHealthy(_ time.Duration) bool { return s.healthy }

func TestHealthz(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("/healthz returned %d, want 200", rr.Code)
	}
}

func TestReadyz_Healthy(t *testing.T) {
	checker := &stubChecker{healthy: true}
	srv := httptest.NewServer(buildMux(checker))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz returned %d, want 200", resp.StatusCode)
	}
}

func TestReadyz_Unhealthy(t *testing.T) {
	checker := &stubChecker{healthy: false}
	srv := httptest.NewServer(buildMux(checker))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("/readyz returned %d, want 503", resp.StatusCode)
	}
}

// buildMux constructs the same mux that Serve uses, for test access.
func buildMux(checker health.WriteHealthChecker) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if checker.WriteHealthy(10 * time.Minute) {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	return mux
}
