package unit_test

import (
	"math"
	"strings"
	"testing"
	"time"

	liveness "github.com/hyz-is/arandu-liveness"
)

func TestConfigValidateRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	valid := liveness.Config{ModelsDir: "models", EnablePassive: true}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a minimal config is refused: %v", err)
	}
	if err := (liveness.Config{}).Validate(); err != nil {
		t.Errorf("a config with no model needs no models directory, and was refused: %v", err)
	}
	for name, tc := range map[string]struct {
		mutate func(*liveness.Config)
		field  string
	}{
		"no models dir":       {func(c *liveness.Config) { c.ModelsDir = "" }, "ModelsDir"},
		"unknown PAD model":   {func(c *liveness.Config) { c.PADModels = []string{"minifasnet_v9"} }, "PADModels"},
		"PAD model twice":     {func(c *liveness.Config) { c.PADModels = []string{"minifasnet_v2", "minifasnet_v2"} }, "twice"},
		"unknown scale model": {func(c *liveness.Config) { c.Scales = map[string]float64{"x": 2} }, "Scales"},
		"zero scale":          {func(c *liveness.Config) { c.Scales = map[string]float64{liveness.ModelMiniFASNetV2: 0} }, "Scales"},
		"NaN scale":           {func(c *liveness.Config) { c.Scales = map[string]float64{liveness.ModelMiniFASNetV2: math.NaN()} }, "Scales"},
		"huge scale":          {func(c *liveness.Config) { c.Scales = map[string]float64{liveness.ModelMiniFASNetV2: 11} }, "Scales"},
		"reference above one": {func(c *liveness.Config) {
			c.PADReference = liveness.MiniFASNetUpstreamReference()
			c.PADReference.Threshold = 1.5
		}, "Threshold"},
		"challenge with one frame": {func(c *liveness.Config) {
			c.Challenge = liveness.DefaultChallengeThresholds()
			c.Challenge.MinFrames = 1
		}, "MinFrames"},
		"challenge with an empty duration range": {func(c *liveness.Config) {
			c.Challenge = liveness.DefaultChallengeThresholds()
			c.Challenge.MaxMotionDuration = c.Challenge.MinMotionDuration
		}, "MotionDuration"},
		"integrity with a negative jitter": {func(c *liveness.Config) {
			c.Integrity = liveness.DefaultIntegrityThresholds()
			c.Integrity.MinJitterCV = -1
		}, "MinJitterCV"},
		"integrity with no interval": {func(c *liveness.Config) {
			c.Integrity = liveness.DefaultIntegrityThresholds()
			c.Integrity.MaxInterval = 0
		}, "MaxInterval"},
	} {
		c := valid
		tc.mutate(&c)
		err := c.Validate()
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: %v does not name %s", name, err, tc.field)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	t.Parallel()

	c := liveness.Config{ModelsDir: "m", EnablePassive: true}.WithDefaults()
	if len(c.PADModels) != 2 || c.PADModels[0] != liveness.ModelMiniFASNetV2 || c.PADModels[1] != liveness.ModelMiniFASNetV1SE {
		t.Errorf("default PAD models %v", c.PADModels)
	}
	if c.Scales[liveness.ModelMiniFASNetV2] != 2.7 || c.Scales[liveness.ModelMiniFASNetV1SE] != 4.0 {
		t.Errorf("default scales %v", c.Scales)
	}
	if c.PADReference != liveness.MiniFASNetUpstreamReference() || c.Challenge != liveness.DefaultChallengeThresholds() ||
		c.Integrity != liveness.DefaultIntegrityThresholds() {
		t.Errorf("default references %+v %+v %+v", c.PADReference, c.Challenge, c.Integrity)
	}
	if c.PADReference.Version != "minifasnet-upstream-0.5" || c.PADReference.Threshold != 0.5 || c.PADReference.ExpectedAspect != 0.75 {
		t.Errorf("the upstream reference is %+v", c.PADReference)
	}
	if !strings.HasSuffix(c.Challenge.Version, "uncalibrated") || !strings.HasSuffix(c.Integrity.Version, "uncalibrated") {
		t.Errorf("an uncalibrated reference does not say so: %s, %s", c.Challenge.Version, c.Integrity.Version)
	}
	if c.Challenge.MinMotionDuration != 300*time.Millisecond || c.Integrity.MaxInterval != 250*time.Millisecond {
		t.Errorf("durations %v %v", c.Challenge.MinMotionDuration, c.Integrity.MaxInterval)
	}

	own := map[string]float64{liveness.ModelMiniFASNetV1SE: 3.5}
	models := []string{liveness.ModelMiniFASNetV1SE}
	set := liveness.Config{ModelsDir: "m", PADModels: models, Scales: own}.WithDefaults()
	if len(set.PADModels) != 1 || set.Scales[liveness.ModelMiniFASNetV1SE] != 3.5 || set.Scales[liveness.ModelMiniFASNetV2] != 2.7 {
		t.Errorf("WithDefaults overwrote a set field: %+v", set)
	}
	set.Scales[liveness.ModelMiniFASNetV1SE] = 9
	set.PADModels[0] = "changed"
	if own[liveness.ModelMiniFASNetV1SE] != 3.5 || models[0] != liveness.ModelMiniFASNetV1SE {
		t.Error("WithDefaults shares a map or a slice with the configuration it was given")
	}
}
