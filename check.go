package liveness

import (
	"fmt"
	"math"
)

// Check is one measured signal held against the value of a versioned
// reference set. It records an observation about the signal, not a
// conclusion about the capture: what a value outside its reference means for
// a case is the application's policy.
type Check struct {
	// Signal names the measurement.
	Signal string
	// Value is the measurement. A yes-or-no measurement is 1 or 0.
	Value float64
	// Rule is the reference, written out, such as ">= 5" or "in [0.002, 0.5]".
	Rule string
	// WithinReference is whether Value satisfies Rule.
	WithinReference bool
}

// atLeast is the check value >= limit.
func atLeast(signal string, value, limit float64) Check {
	return Check{Signal: signal, Value: value, Rule: fmt.Sprintf(">= %g", limit), WithinReference: value >= limit}
}

// atMost is the check value <= limit.
func atMost(signal string, value, limit float64) Check {
	return Check{Signal: signal, Value: value, Rule: fmt.Sprintf("<= %g", limit), WithinReference: value <= limit}
}

// absAtMost is the check |value| <= limit.
func absAtMost(signal string, value, limit float64) Check {
	return Check{Signal: signal, Value: value, Rule: fmt.Sprintf("|x| <= %g", limit), WithinReference: math.Abs(value) <= limit}
}

// between is the check lo <= value <= hi.
func between(signal string, value, lo, hi float64) Check {
	return Check{
		Signal: signal, Value: value, Rule: fmt.Sprintf("in [%g, %g]", lo, hi),
		WithinReference: value >= lo && value <= hi,
	}
}

// isTrue is the check of a yes-or-no measurement that should be yes.
func isTrue(signal string, b bool) Check {
	return Check{Signal: signal, Value: boolValue(b), Rule: "== 1", WithinReference: b}
}

// isFalse is the check of a yes-or-no measurement that should be no.
func isFalse(signal string, b bool) Check {
	return Check{Signal: signal, Value: boolValue(b), Rule: "== 0", WithinReference: !b}
}

// boolValue is 1 for true and 0 for false.
func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// finite reports whether every value is neither NaN nor infinite.
func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}
