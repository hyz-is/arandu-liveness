package unit_test

import (
	"strings"
	"testing"

	liveness "github.com/hyz-is/arandu-liveness"
)

// TestTheDeepfakeSignalRecordsItsAbsence holds that the stage reports the
// missing detector explicitly, with a reason and a version, and names no
// model: an absent signal recorded as evidence, never a silent skip.
func TestTheDeepfakeSignalRecordsItsAbsence(t *testing.T) {
	t.Parallel()

	s := liveness.DeepfakeUnavailable()
	if s.Availability != liveness.NotAvailable || string(liveness.NotAvailable) != "not_available" {
		t.Errorf("availability %q", s.Availability)
	}
	if s.Model != nil {
		t.Errorf("a model is named: %+v", s.Model)
	}
	if s.Version != liveness.DeepfakeReferenceVersion || s.Version == "" {
		t.Errorf("version %q", s.Version)
	}
	for _, want := range []string{"licence", "training data", "provenance"} {
		if !strings.Contains(s.Reason, want) {
			t.Errorf("the reason does not mention %q: %s", want, s.Reason)
		}
	}
}
