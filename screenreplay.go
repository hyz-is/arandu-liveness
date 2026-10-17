package liveness

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"math/bits"

	onnx "github.com/hyz-is/arandu-onnx"
)

// ScreenReplayMethod names how MeasureScreenReplay measures, and changes
// with any change to the measurement.
const ScreenReplayMethod = "arandu-liveness-screen-replay-fft-band-peak-specular-0.1"

// The bounds of ScreenReplayReferenceSet.AnalysisSize.
const (
	// MinScreenReplaySize is the smallest side the spectrum is taken over,
	// and so the smallest region MeasureScreenReplay measures.
	MinScreenReplaySize = 32
	// MaxScreenReplaySize is the largest side the spectrum is taken over.
	MaxScreenReplaySize = 1024
)

// ScreenReplayReferenceSet is the versioned set of parameters the screen
// replay signals are measured with. It carries no threshold: the method is
// experimental and has not been calibrated on any attack data, and its
// Status says so.
type ScreenReplayReferenceSet struct {
	// Version names this set of values. A change to any value is a new
	// version.
	Version string
	// Status says how far the values can be relied on.
	Status string
	// AnalysisSize is the side, a power of two, of the square grid the
	// region is reduced to before its spectrum is taken. A region smaller
	// than it is analysed at the largest power of two that fits.
	AnalysisSize int
	// BandLow and BandHigh bound the radial spatial frequencies, in cycles
	// per analysed pixel, a periodic peak is looked for between: above the
	// low frequencies of the face's shape and lighting, up to Nyquist.
	BandLow, BandHigh float64
	// HighlightLuma is the BT.601 luma, 0..255, at or above which a pixel
	// is bright enough to count as a specular highlight.
	HighlightLuma float64
	// HighlightMaxChroma is the largest spread between a pixel's largest
	// and smallest channel, 0..255, for it to count as a highlight: a
	// reflection is close to the colour of the light, white.
	HighlightMaxChroma float64
	// Source is where the values come from.
	Source string
}

// DefaultScreenReplayReference returns the parameters MeasureScreenReplay
// uses when the configuration names none. They are starting values, chosen
// by hand, and not calibrated on any population, device or attack.
func DefaultScreenReplayReference() ScreenReplayReferenceSet {
	return ScreenReplayReferenceSet{
		Version:            "arandu-liveness-screen-replay-0.1-experimental-uncalibrated",
		Status:             "experimental, uncalibrated: no threshold, not evaluated on any presentation attack data",
		AnalysisSize:       256,
		BandLow:            0.08,
		BandHigh:           0.5,
		HighlightLuma:      240,
		HighlightMaxChroma: 24,
		Source:             "hand-chosen starting values for a Hann-windowed 2D FFT periodicity measure and a near-white highlight count",
	}
}

// Validate reports a set of parameters that cannot be measured with.
func (r ScreenReplayReferenceSet) Validate() error {
	if r.Version == "" {
		return errors.New("liveness: ScreenReplayReferenceSet.Version is required: a reference nobody can name is one nobody can audit")
	}
	if n := r.AnalysisSize; n < MinScreenReplaySize || n > MaxScreenReplaySize || bits.OnesCount(uint(n)) != 1 {
		return fmt.Errorf("liveness: ScreenReplayReferenceSet.AnalysisSize is %d, and has to be a power of two between %d and %d", n, MinScreenReplaySize, MaxScreenReplaySize)
	}
	if !finite(r.BandLow, r.BandHigh) || r.BandLow <= 0 || r.BandHigh <= r.BandLow || r.BandHigh > math.Sqrt2/2 {
		return fmt.Errorf("liveness: ScreenReplayReferenceSet.BandLow %v and BandHigh %v do not bound a band in (0, %.4f]", r.BandLow, r.BandHigh, math.Sqrt2/2)
	}
	for _, f := range []struct {
		name  string
		value float64
	}{{"HighlightLuma", r.HighlightLuma}, {"HighlightMaxChroma", r.HighlightMaxChroma}} {
		if !finite(f.value) || f.value < 0 || f.value > 255 {
			return fmt.Errorf("liveness: ScreenReplayReferenceSet.%s is %v, and has to be between 0 and 255", f.name, f.value)
		}
	}
	return nil
}

// ScreenReplaySignals is what MeasureScreenReplay returns: how strongly the
// region's texture repeats at one spatial frequency, as the pixel grid of a
// screen and the moiré it makes with the camera's sensor do, and how much of
// it is near-white highlight, as the glare on a screen's glass is. They are
// experimental signals, not a conclusion: printed halftones, fabrics,
// window blinds and compression blocks repeat too, and a sharp image of a
// screen taken at a distance may show no peak at all.
type ScreenReplaySignals struct {
	// Region is the part of the image measured, in the image's coordinates.
	Region image.Rectangle
	// AnalysisSize is the side of the grid the region was reduced to.
	AnalysisSize int
	// PeriodicPeakRatio is the share of the region's spectral energy, the
	// mean left out, held by its strongest frequency in the band: the peak
	// bin and its eight neighbours, which the window spreads it over, and
	// their mirror. Near 1 for a single pure grating, near 0 for a smooth
	// region; 0 when the region is flat.
	PeriodicPeakRatio float64
	// BandEnergyRatio is the share of the spectral energy, the mean left
	// out, in the band.
	BandEnergyRatio float64
	// PeakFrequency is the radial frequency of the strongest bin in the
	// band, in cycles per analysed pixel.
	PeakFrequency float64
	// PeakOrientation is the direction the strongest frequency varies along,
	// in degrees in [0, 180) from the image's x axis toward its y axis
	// (downward): 0 for stripes that change from left to right, 90 for
	// stripes that change from top to bottom, 45 for stripes that change
	// toward the bottom right.
	PeakOrientation float64
	// SpecularHighlightRatio is the share of the region's pixels at least
	// HighlightLuma bright with a channel spread of at most
	// HighlightMaxChroma.
	SpecularHighlightRatio float64
	// MeanLuma is the region's mean BT.601 luma, 0..255.
	MeanLuma float64
	// Method is ScreenReplayMethod.
	Method string
	// Version is the reference set's version, repeated so a record of the
	// signal names it on its own.
	Version string
	// ReferenceSet is the parameters the signals were measured with.
	ReferenceSet ScreenReplayReferenceSet
}

// MeasureScreenReplay measures, in pure Go and without a model, signals of a
// face presented on a screen: a periodic texture and specular highlights. The
// region is normally the face crop, in the image's coordinates; an empty
// region measures the whole image.
//
// The region's luma is reduced by area averaging to a square grid of
// AnalysisSize (or the largest power of two that fits in the region), its
// mean subtracted, a periodic Hann window applied on both axes, and its 2D
// spectrum taken with a radix-2 FFT. The band [BandLow, BandHigh] of radial
// frequencies is searched for its strongest bin, and PeriodicPeakRatio,
// BandEnergyRatio, PeakFrequency and PeakOrientation are read from it. The
// highlights are counted on the region at full resolution. The result is
// deterministic. Area averaging removes a grid finer than the reduction
// factor, so a region much larger than AnalysisSize hides the finest
// textures.
func MeasureScreenReplay(img image.Image, region image.Rectangle, ref ScreenReplayReferenceSet) (ScreenReplaySignals, error) {
	if err := ref.Validate(); err != nil {
		return ScreenReplaySignals{}, err
	}
	if img == nil || img.Bounds().Empty() {
		return ScreenReplaySignals{}, errors.New("liveness: MeasureScreenReplay of an empty image")
	}
	b := img.Bounds()
	if int64(b.Dx())*int64(b.Dy()) > onnx.MaxImagePixels {
		return ScreenReplaySignals{}, fmt.Errorf("%w: MeasureScreenReplay of a %dx%d image, more than %d pixels", onnx.ErrImageTooLarge, b.Dx(), b.Dy(), onnx.MaxImagePixels)
	}
	if region.Empty() {
		region = b
	}
	region = region.Intersect(b)
	w, h := region.Dx(), region.Dy()
	if w < MinScreenReplaySize || h < MinScreenReplaySize {
		return ScreenReplaySignals{}, fmt.Errorf("liveness: the region %v inside the image %v is smaller than %dx%d", region, b, MinScreenReplaySize, MinScreenReplaySize)
	}
	n := ref.AnalysisSize
	for n > w || n > h {
		n /= 2
	}

	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), img, region.Min, draw.Src)
	s := ScreenReplaySignals{Region: region, AnalysisSize: n, Method: ScreenReplayMethod, Version: ref.Version, ReferenceSet: ref}

	luma := make([]float64, w*h)
	highlights := 0
	sum := 0.0
	for y := 0; y < h; y++ {
		row := rgba.Pix[y*rgba.Stride:]
		for x := 0; x < w; x++ {
			p := row[x*4:]
			r, g, bl := float64(p[0]), float64(p[1]), float64(p[2])
			l := 0.299*r + 0.587*g + 0.114*bl
			luma[y*w+x] = l
			sum += l
			if l >= ref.HighlightLuma && math.Max(r, math.Max(g, bl))-math.Min(r, math.Min(g, bl)) <= ref.HighlightMaxChroma {
				highlights++
			}
		}
	}
	s.MeanLuma = sum / float64(w*h)
	s.SpecularHighlightRatio = float64(highlights) / float64(w*h)

	grid := areaAverage(luma, w, h, n)
	spectrum := powerSpectrum(grid, n)
	s.measureSpectrum(spectrum, n, ref)
	return s, nil
}

// MeasureScreenReplay is the package's MeasureScreenReplay with the
// configured reference set. It needs no boot.
func (m *Module) MeasureScreenReplay(img image.Image, region image.Rectangle) (ScreenReplaySignals, error) {
	return MeasureScreenReplay(img, region, m.cfg.ScreenReplay)
}

// areaAverage reduces a w by h grid of values to n by n, each cell the mean
// of the values in its footprint; w and h are at least n.
func areaAverage(values []float64, w, h, n int) []float64 {
	out := make([]float64, n*n)
	for v := 0; v < n; v++ {
		y0, y1 := v*h/n, (v+1)*h/n
		for u := 0; u < n; u++ {
			x0, x1 := u*w/n, (u+1)*w/n
			sum := 0.0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					sum += values[y*w+x]
				}
			}
			out[v*n+u] = sum / float64((y1-y0)*(x1-x0))
		}
	}
	return out
}

// powerSpectrum is the squared magnitude of the 2D DFT of an n by n grid,
// its mean subtracted and a periodic Hann window applied on both axes;
// index ky·n + kx.
func powerSpectrum(grid []float64, n int) []float64 {
	mean := 0.0
	for _, v := range grid {
		mean += v
	}
	mean /= float64(len(grid))
	window := make([]float64, n)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	data := make([]complex128, n*n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			data[y*n+x] = complex((grid[y*n+x]-mean)*window[x]*window[y], 0)
		}
	}
	plan := newFFT(n)
	for y := 0; y < n; y++ {
		plan.transform(data[y*n : (y+1)*n])
	}
	column := make([]complex128, n)
	for x := 0; x < n; x++ {
		for y := 0; y < n; y++ {
			column[y] = data[y*n+x]
		}
		plan.transform(column)
		for y := 0; y < n; y++ {
			data[y*n+x] = column[y]
		}
	}
	power := make([]float64, n*n)
	for i, c := range data {
		power[i] = real(c)*real(c) + imag(c)*imag(c)
	}
	return power
}

// frequency is bin k of an n-point DFT as a signed frequency, in cycles per
// sample.
func frequency(k, n int) float64 {
	if k > n/2 {
		k -= n
	}
	return float64(k) / float64(n)
}

// measureSpectrum fills the spectral signals from the power spectrum.
func (s *ScreenReplaySignals) measureSpectrum(power []float64, n int, ref ScreenReplayReferenceSet) {
	total, band := 0.0, 0.0
	peak, peakValue := -1, 0.0
	for ky := 0; ky < n; ky++ {
		fy := frequency(ky, n)
		for kx := 0; kx < n; kx++ {
			if kx == 0 && ky == 0 {
				continue
			}
			p := power[ky*n+kx]
			total += p
			r := math.Hypot(frequency(kx, n), fy)
			if r < ref.BandLow || r > ref.BandHigh {
				continue
			}
			band += p
			if p > peakValue {
				peak, peakValue = ky*n+kx, p
			}
		}
	}
	if total <= 0 || peak < 0 {
		return
	}
	s.BandEnergyRatio = band / total
	px, py := peak%n, peak/n
	fx, fy := frequency(px, n), frequency(py, n)
	s.PeakFrequency = math.Hypot(fx, fy)
	angle := math.Atan2(fy, fx) * 180 / math.Pi
	angle = math.Mod(angle+360, 180)
	if angle >= 180 {
		angle = 0
	}
	s.PeakOrientation = angle

	// The peak's 3×3 neighbourhood and its mirror's, each bin once.
	counted := map[int]bool{0: true}
	held := 0.0
	for _, centre := range [][2]int{{px, py}, {(n - px) % n, (n - py) % n}} {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				i := ((centre[1]+dy+n)%n)*n + (centre[0]+dx+n)%n
				if !counted[i] {
					counted[i] = true
					held += power[i]
				}
			}
		}
	}
	s.PeriodicPeakRatio = held / total
}

// fft is a radix-2 decimation-in-time FFT of one power-of-two size.
type fft struct {
	n       int
	reverse []int
	twiddle []complex128
}

// newFFT prepares the bit-reversal permutation and the twiddle factors of an
// n-point FFT; n is a power of two.
func newFFT(n int) *fft {
	f := &fft{n: n, reverse: make([]int, n), twiddle: make([]complex128, n/2)}
	shift := bits.UintSize - bits.Len(uint(n-1))
	for i := range f.reverse {
		f.reverse[i] = int(bits.Reverse(uint(i)) >> shift)
	}
	if n == 1 {
		f.reverse[0] = 0
	}
	for k := range f.twiddle {
		angle := -2 * math.Pi * float64(k) / float64(n)
		f.twiddle[k] = complex(math.Cos(angle), math.Sin(angle))
	}
	return f
}

// transform replaces x, of length n, with its DFT: X[k] = Σ x[j]·e^(−2πijk/n).
func (f *fft) transform(x []complex128) {
	for i, j := range f.reverse {
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= f.n; size <<= 1 {
		half, step := size/2, f.n/size
		for start := 0; start < f.n; start += size {
			for k := 0; k < half; k++ {
				t := f.twiddle[k*step] * x[start+k+half]
				x[start+k+half] = x[start+k] - t
				x[start+k] += t
			}
		}
	}
}
