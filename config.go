package liveness

import (
	"errors"
	"fmt"
)

// MaxScale is the largest crop scale a Config may set.
const MaxScale = 10

// Config is what the application passes when it wires this package.
//
// Every value comes from the application's own typed configuration. This
// package reads no environment variable. A field left at its zero value, and
// a reference set left without a version, takes the default named beside it.
type Config struct {
	// ModelsDir is the directory the model files are in, laid out the way
	// their manifests' file fields say (liveness/…). cmd/liveness-models
	// fills one. It is required when EnablePassive is set.
	ModelsDir string

	// EnablePassive opens the PAD models at Boot, which PassivePAD needs.
	EnablePassive bool
	// RefuseDevOnly makes Boot refuse every configured model whose manifest
	// is dev-only, before reading any file: the production switch.
	RefuseDevOnly bool

	// PADModels are the PAD models PassivePAD runs and fuses, in order.
	// Default: ModelMiniFASNetV2 and ModelMiniFASNetV1SE, as upstream fuses
	// them.
	PADModels []string
	// Scales overrides the crop scale of a PAD model, by identifier. A model
	// not named keeps DefaultScales' value, the one it was trained with; any
	// other value leaves upstream's preprocessing, and the result records it.
	Scales map[string]float64
	// MaxPADFrames is the most frames PassivePADFrames runs the models on in
	// one call, which bounds the CPU a capture costs. Default
	// DefaultMaxPADFrames; at most MaxFrames.
	MaxPADFrames int

	// PADReference is the reference set PassivePAD returns. Default
	// MiniFASNetUpstreamReference().
	PADReference PADReferenceSet
	// Challenge are the references EvaluateChallenge checks against. Default
	// DefaultChallengeThresholds().
	Challenge ChallengeThresholds
	// Integrity are the references MeasureInjectionSignals checks against.
	// Default DefaultIntegrityThresholds().
	Integrity IntegrityThresholds
	// Sequence are the references EvaluateChallengeSequence checks against.
	// Default DefaultSequenceThresholds().
	Sequence SequenceThresholds
	// ScreenReplay is the reference set MeasureScreenReplay measures with.
	// Default DefaultScreenReplayReference().
	ScreenReplay ScreenReplayReferenceSet
}

// Validate reports what the configuration cannot be used with. New calls it,
// so a setting that cannot work fails where it is wired.
func (c Config) Validate() error {
	if c.EnablePassive && c.ModelsDir == "" {
		return errors.New("liveness: Config.ModelsDir is required with EnablePassive: it is the directory the model files are in")
	}
	defaults := DefaultScales()
	seen := map[string]bool{}
	for _, id := range c.PADModels {
		if _, ok := defaults[id]; !ok {
			return fmt.Errorf("liveness: Config.PADModels names %q, which is not a PAD model this package ships", id)
		}
		if seen[id] {
			return fmt.Errorf("liveness: Config.PADModels names %q twice", id)
		}
		seen[id] = true
	}
	for id, scale := range c.Scales {
		if _, ok := defaults[id]; !ok {
			return fmt.Errorf("liveness: Config.Scales names %q, which is not a PAD model this package ships", id)
		}
		if !finite(scale) || scale <= 0 || scale > MaxScale {
			return fmt.Errorf("liveness: Config.Scales[%q] is %v, and has to be above 0 and at most %d", id, scale, MaxScale)
		}
	}
	if c.MaxPADFrames < 0 || c.MaxPADFrames > MaxFrames {
		return fmt.Errorf("liveness: Config.MaxPADFrames is %d, and has to be between 1 and %d, or 0 for the default", c.MaxPADFrames, MaxFrames)
	}
	if c.PADReference.Version != "" {
		if err := c.PADReference.Validate(); err != nil {
			return err
		}
	}
	if c.Challenge.Version != "" {
		if err := c.Challenge.Validate(); err != nil {
			return err
		}
	}
	if c.Integrity.Version != "" {
		if err := c.Integrity.Validate(); err != nil {
			return err
		}
	}
	if c.Sequence.Version != "" {
		if err := c.Sequence.Validate(); err != nil {
			return err
		}
	}
	if c.ScreenReplay.Version != "" {
		if err := c.ScreenReplay.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// WithDefaults returns the configuration with every zero field set to its
// default. It does not validate, and it never shares a slice or a map with
// the configuration it was given.
func (c Config) WithDefaults() Config {
	if len(c.PADModels) == 0 {
		c.PADModels = []string{ModelMiniFASNetV2, ModelMiniFASNetV1SE}
	} else {
		c.PADModels = append([]string(nil), c.PADModels...)
	}
	scales := DefaultScales()
	for id, scale := range c.Scales {
		scales[id] = scale
	}
	c.Scales = scales
	if c.MaxPADFrames == 0 {
		c.MaxPADFrames = DefaultMaxPADFrames
	}
	if c.PADReference.Version == "" {
		c.PADReference = MiniFASNetUpstreamReference()
	}
	if c.Challenge.Version == "" {
		c.Challenge = DefaultChallengeThresholds()
	}
	if c.Integrity.Version == "" {
		c.Integrity = DefaultIntegrityThresholds()
	}
	if c.Sequence.Version == "" {
		c.Sequence = DefaultSequenceThresholds()
	}
	if c.ScreenReplay.Version == "" {
		c.ScreenReplay = DefaultScreenReplayReference()
	}
	return c
}
