package liveness

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/arandu-io/framework/foundation"
	fhttp "github.com/arandu-io/framework/http"

	onnx "github.com/kuaa-io/arandu-onnx"
)

// The errors a Module reports about its own life and configuration. Each is
// wrapped with the detail of the case, so callers test for them with
// errors.Is.
var (
	// ErrNotBooted is a module used before Boot.
	ErrNotBooted = errors.New("liveness: module not booted")
	// ErrClosed is a module used after Close.
	ErrClosed = errors.New("liveness: module closed")
	// ErrNotEnabled is a stage whose model the configuration did not enable.
	ErrNotEnabled = errors.New("liveness: stage not enabled")
	// ErrDevOnlyRefused is a dev-only model configured while RefuseDevOnly is
	// set.
	ErrDevOnlyRefused = errors.New("liveness: dev-only model refused")
)

// moduleState is where a Module is in its life.
type moduleState int

const (
	stateNew moduleState = iota
	stateReady
	stateClosed
)

// Module is what the application registers, and what runs the model stage.
//
// Besides foundation.Module it implements foundation.Bootable, which opens the
// models, foundation.Health, which reports whether they are open, and
// foundation.Closable, which closes them. PassivePAD is safe for concurrent
// use; Close waits for the calls in flight. The pure-Go stages need no boot.
type Module struct {
	cfg       Config
	runtime   *onnx.Runtime
	manifests map[string]onnx.Manifest

	mu    sync.RWMutex
	state moduleState
	pad   []*onnx.Session
}

// Compile-time proof that the module honors the contracts it claims.
var (
	_ foundation.Module   = (*Module)(nil)
	_ foundation.Bootable = (*Module)(nil)
	_ foundation.Health   = (*Module)(nil)
	_ foundation.Closable = (*Module)(nil)
)

// New returns the module, or the reason it cannot be built.
//
// The runtime is the one arandu-onnx's module hands out. New validates the
// configuration and the embedded manifests and opens nothing: Boot does, so
// building the module in a test or a command that never boots it needs
// neither ONNX Runtime nor the model files.
func New(cfg Config, runtime *onnx.Runtime) (*Module, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if runtime == nil {
		return nil, errors.New("liveness: New needs the ONNX runtime, from the arandu-onnx module's Runtime()")
	}
	cfg = cfg.WithDefaults()

	all, err := Manifests()
	if err != nil {
		return nil, err
	}
	manifests := make(map[string]onnx.Manifest, len(all))
	for _, m := range all {
		manifests[m.ID] = m
	}
	return &Module{cfg: cfg, runtime: runtime, manifests: manifests}, nil
}

// Config returns the configuration the module uses, with defaults filled in.
// The slice and the map are copies.
func (m *Module) Config() Config {
	c := m.cfg
	c.PADModels = append([]string(nil), m.cfg.PADModels...)
	c.Scales = make(map[string]float64, len(m.cfg.Scales))
	for id, s := range m.cfg.Scales {
		c.Scales[id] = s
	}
	return c
}

// Name is the module identifier: a lowercase slug, stable, no spaces.
func (m *Module) Name() string { return "liveness" }

// Routes registers nothing.
//
// Captures are measured through the application's own controllers, behind
// its Grants. A route here would measure any face for anybody who sent one.
func (m *Module) Routes(*fhttp.Router) {}

// wanted is the models the configuration needs, in the order Boot opens
// them.
func (m *Module) wanted() []string {
	if !m.cfg.EnablePassive {
		return nil
	}
	return m.cfg.PADModels
}

// Boot opens the configured models on the runtime, initializing the runtime
// first when arandu-onnx has not booted yet. A configuration with no model
// enabled boots without touching the runtime.
//
// With RefuseDevOnly set, a configured model whose manifest is dev-only stops
// the boot before any file is read. Otherwise each file is verified against
// its manifest before ONNX Runtime reads it; a missing or altered file stops
// the boot, naming the model.
func (m *Module) Boot(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.state {
	case stateReady:
		return nil
	case stateClosed:
		return ErrClosed
	}
	wanted := m.wanted()
	if m.cfg.RefuseDevOnly {
		for _, id := range wanted {
			if manifest := m.manifests[id]; manifest.Status == onnx.StatusDevOnly {
				return fmt.Errorf("%w: %s %s is dev-only and Config.RefuseDevOnly is set", ErrDevOnlyRefused, manifest.ID, manifest.Version)
			}
		}
	}
	if len(wanted) > 0 {
		if err := m.runtime.Init(); err != nil {
			return fmt.Errorf("liveness: initializing the ONNX runtime: %w", err)
		}
	}
	opened := make([]*onnx.Session, 0, len(wanted))
	for _, id := range wanted {
		session, err := m.runtime.Open(m.manifests[id], m.cfg.ModelsDir)
		if err != nil {
			for _, s := range opened {
				_ = s.Close()
			}
			return fmt.Errorf("liveness: opening %s from %s: %w", id, m.cfg.ModelsDir, err)
		}
		opened = append(opened, session)
	}
	m.pad = opened
	m.state = stateReady
	return nil
}

// Health reports whether the module is usable: nil after Boot, ErrNotBooted
// before, ErrClosed after Close, and the runtime's own error when models are
// open and the runtime was closed underneath.
func (m *Module) Health(context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usable()
}

// usable is Health for a caller already holding mu.
func (m *Module) usable() error {
	switch m.state {
	case stateNew:
		return ErrNotBooted
	case stateClosed:
		return ErrClosed
	}
	if len(m.pad) > 0 {
		if err := m.runtime.Health(); err != nil {
			return fmt.Errorf("liveness: the ONNX runtime is not usable: %w", err)
		}
	}
	return nil
}

// Models returns the ModelInfo of every model the module has open, in the
// order Boot opened them, or nothing before Boot and after Close.
func (m *Module) Models() []onnx.ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]onnx.ModelInfo, 0, len(m.pad))
	for _, s := range m.pad {
		out = append(out, s.Model())
	}
	return out
}

// Close closes the module's sessions. It is idempotent, and it waits for the
// calls in flight.
func (m *Module) Close(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == stateClosed {
		return nil
	}
	m.state = stateClosed
	var errs []error
	for _, s := range m.pad {
		errs = append(errs, s.Close())
	}
	m.pad = nil
	return errors.Join(errs...)
}
