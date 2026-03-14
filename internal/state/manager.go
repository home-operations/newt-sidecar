package state

import (
	"log/slog"
	"os"
	"reflect"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
)

// Manager maintains the global state of all blueprint resources.
type Manager struct {
	mu         sync.Mutex
	resources  map[string]blueprint.Resource
	outputFile string
}

// NewManager creates a new state manager.
func NewManager(outputFile string) *Manager {
	return &Manager{
		resources:  make(map[string]blueprint.Resource),
		outputFile: outputFile,
	}
}

// AddOrUpdate adds or updates a resource and writes state if changed.
func (m *Manager) AddOrUpdate(key string, r blueprint.Resource, write bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.resources[key]
	if exists && reflect.DeepEqual(existing, r) {
		return false
	}

	m.resources[key] = r

	if write {
		m.writeState()
	}

	return true
}

// Remove removes a resource and writes state if changed.
func (m *Manager) Remove(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.resources[key]
	if !exists {
		return false
	}

	delete(m.resources, key)
	m.writeState()
	return true
}

// ForceWrite forces a write of the current state to disk.
func (m *Manager) ForceWrite() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeState()
}

// writeState writes the current state to disk (must be called with mutex held).
func (m *Manager) writeState() {
	bp := blueprint.Blueprint{
		PublicResources: m.resources,
	}

	yamlData, err := yaml.Marshal(bp)
	if err != nil {
		slog.Error("failed to marshal blueprint to yaml", "error", err)
		return
	}

	tmp := m.outputFile + ".tmp"
	if err := os.WriteFile(tmp, yamlData, 0o644); err != nil {
		slog.Error("failed to write blueprint to temp file", "error", err)
		return
	}

	if err := os.Rename(tmp, m.outputFile); err != nil {
		slog.Error("failed to rename blueprint temp file", "error", err)
		_ = os.Remove(tmp)
		return
	}

	slog.Info("wrote blueprint file", "file", m.outputFile, "resources", len(m.resources))
}
