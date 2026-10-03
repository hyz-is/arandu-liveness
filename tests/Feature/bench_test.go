package feature_test

import (
	"context"
	"testing"

	liveness "github.com/kuaa-io/arandu-liveness"
)

// The benchmark times PassivePAD, both models, on the 480×640 cartoon, one
// call at a time:
// go test -run '^$' -bench . ./tests/Feature/

func BenchmarkPassivePAD(b *testing.B) {
	m := bootModule(b, liveness.Config{})
	img := cartoonFace(480, 640, 240, 300, 95)
	box := drawnBox(240, 300, 95)
	for b.Loop() {
		if _, err := m.PassivePAD(context.Background(), img, box); err != nil {
			b.Fatal(err)
		}
	}
}
