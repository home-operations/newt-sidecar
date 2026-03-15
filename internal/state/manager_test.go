package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/state"
)

func makeResource(name, hostname string) blueprint.Resource {
	return blueprint.Resource{
		Name:          name,
		Protocol:      "http",
		SSL:           true,
		FullDomain:    hostname,
		TLSServerName: hostname,
		Targets: []blueprint.Target{
			{Site: "test-site", Hostname: "gw.local", Method: "https", Port: 443},
		},
	}
}

func TestManager_AddOrUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "blueprint.yaml")
	m := state.NewManager(outputFile)

	tests := []struct {
		name       string
		key        string
		resource   blueprint.Resource
		write      bool
		wantChange bool
	}{
		{
			name:       "add new resource",
			key:        "home-example-com",
			resource:   makeResource("home-assistant", "home.example.com"),
			write:      false,
			wantChange: true,
		},
		{
			name:       "update existing resource",
			key:        "home-example-com",
			resource:   makeResource("home-assistant-v2", "home.example.com"),
			write:      false,
			wantChange: true,
		},
		{
			name:       "no change for same resource",
			key:        "home-example-com",
			resource:   makeResource("home-assistant-v2", "home.example.com"),
			write:      false,
			wantChange: false,
		},
		{
			name:       "add another resource",
			key:        "ws-example-com",
			resource:   makeResource("webhook-receiver", "ws.example.com"),
			write:      false,
			wantChange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := m.AddOrUpdate(tt.key, tt.resource, tt.write)
			if changed != tt.wantChange {
				t.Errorf("AddOrUpdate() changed = %v, want %v", changed, tt.wantChange)
			}
		})
	}
}

func TestManager_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "blueprint.yaml")
	m := state.NewManager(outputFile)

	m.AddOrUpdate("home-example-com", makeResource("home-assistant", "home.example.com"), false)

	tests := []struct {
		name       string
		key        string
		wantChange bool
	}{
		{"remove existing", "home-example-com", true},
		{"remove non-existent", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := m.Remove(tt.key)
			if changed != tt.wantChange {
				t.Errorf("Remove() changed = %v, want %v", changed, tt.wantChange)
			}
		})
	}
}

func TestManager_ForceWrite(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "blueprint.yaml")
	m := state.NewManager(outputFile)

	m.AddOrUpdate("home-example-com", makeResource("home-assistant", "home.example.com"), false)
	m.AddOrUpdate("ws-example-com", makeResource("webhook-receiver", "ws.example.com"), false)
	m.ForceWrite()

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "public-resources") {
		t.Error("Output should contain 'public-resources'")
	}
	if !strings.Contains(content, "home-assistant") {
		t.Error("Output should contain 'home-assistant'")
	}
	if !strings.Contains(content, "webhook-receiver") {
		t.Error("Output should contain 'webhook-receiver'")
	}
}

func TestManager_NoWriteOnNoChange(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "blueprint.yaml")
	m := state.NewManager(outputFile)

	r := makeResource("home-assistant", "home.example.com")
	m.AddOrUpdate("home-example-com", r, true)

	data1, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	m.AddOrUpdate("home-example-com", r, true)

	data2, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if string(data1) != string(data2) {
		t.Error("File should not change when resource is unchanged")
	}
}

func TestManager_WriteHealthy(t *testing.T) {
	t.Run("healthy before any write (grace period)", func(t *testing.T) {
		tmpDir := t.TempDir()
		m := state.NewManager(filepath.Join(tmpDir, "blueprint.yaml"))
		if !m.WriteHealthy(10 * time.Minute) {
			t.Error("WriteHealthy should return true before any write attempt")
		}
	})

	t.Run("healthy after successful write within threshold", func(t *testing.T) {
		tmpDir := t.TempDir()
		m := state.NewManager(filepath.Join(tmpDir, "blueprint.yaml"))
		m.ForceWrite()
		if !m.WriteHealthy(10 * time.Minute) {
			t.Error("WriteHealthy should return true immediately after a successful write")
		}
	})

	t.Run("unhealthy after threshold exceeded", func(t *testing.T) {
		tmpDir := t.TempDir()
		m := state.NewManager(filepath.Join(tmpDir, "blueprint.yaml"))
		m.ForceWrite()
		// Use a threshold of 0 to simulate expiry.
		if m.WriteHealthy(0) {
			t.Error("WriteHealthy should return false when threshold is exceeded")
		}
	})

	t.Run("unhealthy after write error", func(t *testing.T) {
		// Use a path that cannot be written to trigger a write error.
		m := state.NewManager("/nonexistent-dir/blueprint.yaml")
		m.ForceWrite()
		if m.WriteHealthy(10 * time.Minute) {
			t.Error("WriteHealthy should return false after a write error")
		}
	})
}

func TestManager_ConcurrentOperations(t *testing.T) {
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "blueprint.yaml")
	m := state.NewManager(outputFile)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(_ int) {
			m.AddOrUpdate("concurrent-key", makeResource("concurrent-test", "concurrent.example.com"), false)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	m.ForceWrite()

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if len(data) == 0 {
		t.Error("Output file is empty after concurrent operations")
	}
}
