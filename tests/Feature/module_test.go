package feature_test

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/foundation"
	fhttp "github.com/arandu-io/framework/http"

	face "github.com/hyz-is/arandu-face"
	liveness "github.com/hyz-is/arandu-liveness"
	onnx "github.com/hyz-is/arandu-onnx"
)

// These tests drive the module the way an application does: build it,
// register it, boot it, ask its health and close it.

// unbootedModule builds the module on a runtime that loads nothing.
func unbootedModule(t *testing.T, cfg liveness.Config) *liveness.Module {
	t.Helper()
	rt, err := onnx.NewRuntime(onnx.Config{})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	m, err := liveness.New(cfg, rt)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}
	return m
}

func TestTheModuleIsRegisteredAsAModule(t *testing.T) {
	t.Parallel()

	var module foundation.Module = unbootedModule(t, liveness.Config{})
	if module.Name() != "liveness" {
		t.Fatalf("Name() = %q, want liveness: route names and the boot log are keyed by it", module.Name())
	}
}

// TestTheModuleServesNoRoute holds the reason Routes is empty: captures are
// measured through the application's own controllers, and a route here would
// measure any face for anybody.
func TestTheModuleServesNoRoute(t *testing.T) {
	t.Parallel()

	m := unbootedModule(t, liveness.Config{})
	router := fhttp.NewRouter()
	m.Routes(router.ForModule(m.Name()))
	if routes := router.Routes(); len(routes) != 0 {
		t.Fatalf("the module registered %d routes, want none: %+v", len(routes), routes)
	}
}

func TestNewRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	rt, err := onnx.NewRuntime(onnx.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := liveness.New(liveness.Config{}, nil); err == nil {
		t.Error("New accepted a nil runtime")
	}
	if _, err := liveness.New(liveness.Config{EnablePassive: true}, rt); err == nil || !strings.Contains(err.Error(), "ModelsDir") {
		t.Errorf("New with passive PAD and no models directory = %v, want an error naming ModelsDir", err)
	}
	if _, err := liveness.New(liveness.Config{PADModels: []string{"x"}}, rt); err == nil {
		t.Error("New accepted an unknown PAD model")
	}
	m, err := liveness.New(liveness.Config{Scales: map[string]float64{liveness.ModelMiniFASNetV2: 3}}, rt)
	if err != nil {
		t.Fatal(err)
	}
	c := m.Config()
	c.Scales[liveness.ModelMiniFASNetV2] = 9
	c.PADModels[0] = "changed"
	if m.Config().Scales[liveness.ModelMiniFASNetV2] != 3 || m.Config().PADModels[0] != liveness.ModelMiniFASNetV2 {
		t.Error("Config hands out the module's own map or slice")
	}
}

// TestRefuseDevOnlyPassesEveryShippedConfiguration is the production switch
// with nothing left to refuse: both PAD models are approved, so no
// configuration stops at the dev-only check. On an empty directory the boot
// goes on to the files, and fails there or at the runtime instead. It needs
// neither ONNX Runtime nor the files, because the check comes first.
//
// A model added as dev-only is refused again with ErrDevOnlyRefused; the
// switch stays for it.
func TestRefuseDevOnlyPassesEveryShippedConfiguration(t *testing.T) {
	t.Parallel()

	manifests, err := liveness.Manifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range manifests {
		if m.Status == onnx.StatusDevOnly {
			t.Errorf("%s is dev-only: extend this test with the configuration that needs it, refused", m.ID)
		}
	}
	ctx := context.Background()
	for _, models := range [][]string{nil, {liveness.ModelMiniFASNetV2}, {liveness.ModelMiniFASNetV1SE}} {
		m := unbootedModule(t, liveness.Config{ModelsDir: t.TempDir(), EnablePassive: true, RefuseDevOnly: true, PADModels: models})
		err := m.Boot(ctx)
		if err == nil || errors.Is(err, liveness.ErrDevOnlyRefused) {
			t.Errorf("Boot of %v on an empty directory = %v, want a missing file or runtime, not a dev-only refusal", models, err)
		}
		if err := m.Health(ctx); !errors.Is(err, liveness.ErrNotBooted) {
			t.Errorf("Health after the failed boot = %v, want ErrNotBooted", err)
		}
	}
}

func TestRefuseDevOnlyBootsBothPADModels(t *testing.T) {
	m := bootModule(t, liveness.Config{EnablePassive: true, RefuseDevOnly: true})
	models := m.Models()
	if len(models) != 2 {
		t.Fatalf("RefuseDevOnly with passive PAD opened %+v, want both PAD models", models)
	}
	for _, info := range models {
		if info.Status != onnx.StatusApproved {
			t.Errorf("%s opened with status %s", info.ID, info.Status)
		}
	}
}

// TestWithoutPassivePADTheModuleBootsWithNoModel holds that the pure-Go
// stages need neither ONNX Runtime nor a model file, and that PassivePAD
// says it was not enabled rather than running nothing.
func TestWithoutPassivePADTheModuleBootsWithNoModel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := unbootedModule(t, liveness.Config{RefuseDevOnly: true})
	if err := m.Boot(ctx); err != nil {
		t.Fatalf("Boot with no model = %v", err)
	}
	if err := m.Health(ctx); err != nil {
		t.Errorf("Health = %v", err)
	}
	if len(m.Models()) != 0 {
		t.Errorf("models %+v", m.Models())
	}
	if _, err := m.PassivePAD(ctx, image.NewRGBA(image.Rect(0, 0, 10, 10)), face.Box{Width: 5, Height: 5}); !errors.Is(err, liveness.ErrNotEnabled) {
		t.Errorf("PassivePAD without EnablePassive = %v, want ErrNotEnabled", err)
	}
	signal, err := m.Deepfake(ctx, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	if err != nil || signal.Availability != liveness.NotAvailable || signal.Model != nil {
		t.Errorf("Deepfake = %+v, %v", signal, err)
	}
	if _, err := m.Deepfake(ctx, image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Error("Deepfake of an empty image was accepted")
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.Deepfake(done, image.NewRGBA(image.Rect(0, 0, 10, 10))); !errors.Is(err, context.Canceled) {
		t.Errorf("Deepfake on a cancelled context = %v", err)
	}

	issued := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ch, err := liveness.NewChallenge(liveness.ChallengeBlink, issued, 10*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.EvaluateChallenge(ch, liveness.ChallengeResponse{EchoedNonce: ch.Nonce, ReceivedAt: issued.Add(time.Second)})
	if err != nil || r.Supported || r.Thresholds != liveness.DefaultChallengeThresholds() {
		t.Errorf("EvaluateChallenge = %+v, %v", r, err)
	}
	s, err := m.MeasureInjectionSignals(liveness.Capture{Frames: []liveness.CaptureFrame{{Timestamp: issued, Image: image.NewRGBA(image.Rect(0, 0, 30, 40))}}})
	if err != nil || s.Thresholds != liveness.DefaultIntegrityThresholds() {
		t.Errorf("MeasureInjectionSignals = %+v, %v", s, err)
	}
}

func TestTheModuleIsUnusableBeforeBootAndAfterClose(t *testing.T) {
	m := bootModule(t, liveness.Config{})
	ctx := context.Background()
	if err := m.Health(ctx); err != nil {
		t.Fatalf("Health after Boot = %v, want nil", err)
	}
	models := m.Models()
	if len(models) != 2 || models[0].ID != liveness.ModelMiniFASNetV2 || models[1].ID != liveness.ModelMiniFASNetV1SE {
		t.Errorf("models open %+v", models)
	}
	if err := m.Boot(ctx); err != nil {
		t.Errorf("a second Boot = %v, want nil", err)
	}
	if err := m.Close(ctx); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := m.Close(ctx); err != nil {
		t.Errorf("a second Close = %v, want nil", err)
	}
	if err := m.Health(ctx); !errors.Is(err, liveness.ErrClosed) {
		t.Errorf("Health after Close = %v, want ErrClosed", err)
	}
	img := cartoonFace(480, 640, 240, 300, 95)
	if _, err := m.PassivePAD(ctx, img, drawnBox(240, 300, 95)); !errors.Is(err, liveness.ErrClosed) {
		t.Errorf("PassivePAD after Close = %v, want ErrClosed", err)
	}
	if err := m.Boot(ctx); !errors.Is(err, liveness.ErrClosed) {
		t.Errorf("Boot after Close = %v, want ErrClosed", err)
	}
	if len(m.Models()) != 0 {
		t.Error("a closed module still lists models")
	}

	fresh := unbootedModule(t, liveness.Config{ModelsDir: t.TempDir(), EnablePassive: true})
	if err := fresh.Health(ctx); !errors.Is(err, liveness.ErrNotBooted) {
		t.Errorf("Health before Boot = %v, want ErrNotBooted", err)
	}
	if _, err := fresh.PassivePAD(ctx, img, drawnBox(240, 300, 95)); !errors.Is(err, liveness.ErrNotBooted) {
		t.Errorf("PassivePAD before Boot = %v, want ErrNotBooted", err)
	}
}

func TestHealthFailsWhenTheRuntimeIsClosedUnderneath(t *testing.T) {
	rt := newRuntime(t)
	m, err := liveness.New(liveness.Config{ModelsDir: modelsDir(t), EnablePassive: true}, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Health(context.Background()); !errors.Is(err, onnx.ErrClosed) {
		t.Errorf("Health with the runtime closed = %v, want onnx.ErrClosed", err)
	}
}

func TestBootRefusesAMissingOrAlteredModel(t *testing.T) {
	rt := newRuntime(t)
	ctx := context.Background()

	m, err := liveness.New(liveness.Config{ModelsDir: t.TempDir(), EnablePassive: true}, rt)
	if err != nil {
		t.Fatal(err)
	}
	err = m.Boot(ctx)
	if err == nil || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), liveness.ModelMiniFASNetV2) {
		t.Errorf("Boot on an empty directory = %v, want a missing file naming %s", err, liveness.ModelMiniFASNetV2)
	}
	if err := m.Health(ctx); !errors.Is(err, liveness.ErrNotBooted) {
		t.Errorf("Health after a failed Boot = %v, want ErrNotBooted", err)
	}

	altered := t.TempDir()
	manifest, err := liveness.LookupManifest(liveness.ModelMiniFASNetV2)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(altered, manifest.File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not the model"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err = liveness.New(liveness.Config{ModelsDir: altered, EnablePassive: true}, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Boot(ctx); !errors.Is(err, onnx.ErrDigestMismatch) {
		t.Errorf("Boot on an altered file = %v, want onnx.ErrDigestMismatch", err)
	}
}
