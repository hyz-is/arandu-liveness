package unit_test

import (
	"context"
	"errors"
	"image"
	"math"
	"strings"
	"testing"

	face "github.com/hyz-is/arandu-face"
	liveness "github.com/hyz-is/arandu-liveness"
	onnx "github.com/hyz-is/arandu-onnx"
)

func TestSummarizePADSeriesGoldenValues(t *testing.T) {
	t.Parallel()

	// 0.2, 0.8, 0.5, 0.6: mean 2.1/4 = 0.525; sorted 0.2, 0.5, 0.6, 0.8, so
	// the median is (0.5+0.6)/2 = 0.55; deviations −0.325, 0.275, −0.025,
	// 0.075 square to 0.105625 + 0.075625 + 0.000625 + 0.005625 = 0.1875,
	// a variance of 0.046875; steps 0.6, 0.3, 0.1, a mean of 1/3.
	s := liveness.SummarizePADSeries([]float64{0.2, 0.8, 0.5, 0.6})
	if s.Count != 4 || !near2(s.Mean, 0.525) || !near2(s.Median, 0.55) || s.Min != 0.2 || s.Max != 0.8 ||
		!near2(s.Variance, 0.046875) || !near2(s.StdDev, math.Sqrt(0.046875)) ||
		!near2(s.MeanAbsStep, 1.0/3) || !near2(s.MaxAbsStep, 0.6) {
		t.Errorf("stats %+v", s)
	}
	if odd := liveness.SummarizePADSeries([]float64{3, 1, 2}); odd.Median != 2 || odd.Mean != 2 {
		t.Errorf("odd count %+v", odd)
	}
	if one := liveness.SummarizePADSeries([]float64{0.7}); one.Count != 1 || one.Median != 0.7 || one.Variance != 0 || one.MaxAbsStep != 0 {
		t.Errorf("one value %+v", one)
	}
	if none := liveness.SummarizePADSeries(nil); none != (liveness.PADSeriesStats{}) {
		t.Errorf("no values %+v", none)
	}
}

func TestEvenlySpacedFramesCoverTheCapture(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		count, limit int
		want         []int
	}{
		// round(i·9/3) for i = 0..3.
		{10, 4, []int{0, 3, 6, 9}},
		// round(i·7/2): 0, round(3.5) = 4, 7.
		{8, 3, []int{0, 4, 7}},
		{5, 8, []int{0, 1, 2, 3, 4}},
		{5, 5, []int{0, 1, 2, 3, 4}},
		{7, 1, []int{0}},
		{7, 0, nil},
		{0, 3, []int{}},
	} {
		if got := liveness.EvenlySpacedFrames(tc.count, tc.limit); !sameInts(got, tc.want) {
			t.Errorf("EvenlySpacedFrames(%d, %d) = %v, want %v", tc.count, tc.limit, got, tc.want)
		}
	}
	for count := 1; count <= 60; count++ {
		for limit := 1; limit <= 10; limit++ {
			got := liveness.EvenlySpacedFrames(count, limit)
			if len(got) != min(count, limit) || got[0] != 0 || got[len(got)-1] != count-1 && limit > 1 {
				t.Fatalf("EvenlySpacedFrames(%d, %d) = %v", count, limit, got)
			}
			for i := 1; i < len(got); i++ {
				if got[i] <= got[i-1] {
					t.Fatalf("EvenlySpacedFrames(%d, %d) = %v is not increasing", count, limit, got)
				}
			}
		}
	}
}

func TestPassivePADFramesNeedsTheModels(t *testing.T) {
	t.Parallel()

	rt, err := onnx.NewRuntime(onnx.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := liveness.New(liveness.Config{}, rt)
	if err != nil {
		t.Fatal(err)
	}
	frames := []liveness.PADFrame{{Image: image.NewRGBA(image.Rect(0, 0, 10, 10)), Box: face.Box{Width: 5, Height: 5}}}
	if _, err := m.PassivePADFrames(context.Background(), frames); !errors.Is(err, liveness.ErrNotBooted) {
		t.Errorf("before Boot = %v, want ErrNotBooted", err)
	}
	// With no model enabled the module boots without ONNX Runtime, and the
	// stage says it is not enabled.
	if err := m.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PassivePADFrames(context.Background(), frames); !errors.Is(err, liveness.ErrNotEnabled) || !strings.Contains(err.Error(), "PassivePADFrames") {
		t.Errorf("without EnablePassive = %v, want ErrNotEnabled naming PassivePADFrames", err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PassivePADFrames(context.Background(), frames); !errors.Is(err, liveness.ErrClosed) {
		t.Errorf("after Close = %v, want ErrClosed", err)
	}
}

func TestTheFrameLimitIsConfigured(t *testing.T) {
	t.Parallel()

	if c := (liveness.Config{}).WithDefaults(); c.MaxPADFrames != liveness.DefaultMaxPADFrames || liveness.DefaultMaxPADFrames != 8 {
		t.Errorf("default frame limit %d", c.MaxPADFrames)
	}
	if c := (liveness.Config{MaxPADFrames: 3}).WithDefaults(); c.MaxPADFrames != 3 {
		t.Errorf("a configured frame limit became %d", c.MaxPADFrames)
	}
	for _, bad := range []int{-1, liveness.MaxFrames + 1} {
		if err := (liveness.Config{MaxPADFrames: bad}).Validate(); err == nil || !strings.Contains(err.Error(), "MaxPADFrames") {
			t.Errorf("MaxPADFrames %d: %v", bad, err)
		}
	}
}
