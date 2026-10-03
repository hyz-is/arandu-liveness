package feature_test

import (
	"context"
	"sync"
	"testing"

	liveness "github.com/kuaa-io/arandu-liveness"
)

// TestPassivePADRunsConcurrently runs PassivePAD from several goroutines on
// one module at once. Under -race it holds that the calls share nothing
// mutable; the comparison with a sequential run holds that sharing the
// sessions does not change an answer. Four workers are enough to make the
// calls overlap without loading the machine.
func TestPassivePADRunsConcurrently(t *testing.T) {
	m := bootModule(t, liveness.Config{})
	ctx := context.Background()
	img := cartoonFace(480, 640, 240, 300, 95)
	box := drawnBox(240, 300, 95)

	want, err := m.PassivePAD(ctx, img, box)
	if err != nil {
		t.Fatal(err)
	}

	const workers, rounds = 4, 3
	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				got, err := m.PassivePAD(ctx, img, box)
				if err != nil {
					errs <- err
					continue
				}
				for i := range want.PerModel {
					if got.PerModel[i].Logits != want.PerModel[i].Logits {
						t.Errorf("a concurrent PassivePAD gave %v for model %d, want %v", got.PerModel[i].Logits, i, want.PerModel[i].Logits)
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
