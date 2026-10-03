package liveness

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"math/bits"
	"sort"
	"time"

	onnx "github.com/kuaa-io/arandu-onnx"
)

// IntegrityThresholds are the references a capture's integrity signals are
// checked against. They are versioned parameters, not a policy.
type IntegrityThresholds struct {
	// Version names this set of values. A change to any value is a new
	// version.
	Version string
	// MinJitterCV and MaxJitterCV bound the coefficient of variation of the
	// intervals between frames. A camera's clock jitters a little; a stream
	// with none at all is as unusual as one with a great deal.
	MinJitterCV, MaxJitterCV float64
	// MaxInterval is the longest gap between two frames.
	MaxInterval time.Duration
	// NearDuplicateBits is the largest DifferenceHash distance, in bits, at
	// which two frames count as near duplicates.
	NearDuplicateBits int
	// MaxNearDuplicateRatio is the largest share of consecutive frame pairs
	// that may be near duplicates.
	MaxNearDuplicateRatio float64
	// ExpectedAspect is the frame width over height the capture is expected
	// to have, and AspectTolerance how far from it a frame may be.
	ExpectedAspect, AspectTolerance float64
}

// DefaultIntegrityThresholds returns the references MeasureInjectionSignals
// uses when the configuration names none. They are starting values, not
// calibrated on any population or device; their version says so.
func DefaultIntegrityThresholds() IntegrityThresholds {
	return IntegrityThresholds{
		Version:               "kuaa-liveness-integrity-0.1-uncalibrated",
		MinJitterCV:           0.002,
		MaxJitterCV:           0.5,
		MaxInterval:           250 * time.Millisecond,
		NearDuplicateBits:     2,
		MaxNearDuplicateRatio: 0.5,
		ExpectedAspect:        UpstreamCaptureAspect,
		AspectTolerance:       0.01,
	}
}

// Validate reports a set of references that cannot be checked against.
func (t IntegrityThresholds) Validate() error {
	if t.Version == "" {
		return errors.New("liveness: IntegrityThresholds.Version is required: a threshold nobody can name is one nobody can audit")
	}
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"MinJitterCV", t.MinJitterCV}, {"MaxJitterCV", t.MaxJitterCV},
		{"MaxNearDuplicateRatio", t.MaxNearDuplicateRatio}, {"AspectTolerance", t.AspectTolerance},
	} {
		if !finite(f.value) || f.value < 0 {
			return fmt.Errorf("liveness: IntegrityThresholds.%s is %v, and has to be a finite value of at least 0", f.name, f.value)
		}
	}
	if t.MinJitterCV > t.MaxJitterCV {
		return fmt.Errorf("liveness: IntegrityThresholds.MinJitterCV %v is above MaxJitterCV %v", t.MinJitterCV, t.MaxJitterCV)
	}
	if t.MaxInterval <= 0 {
		return fmt.Errorf("liveness: IntegrityThresholds.MaxInterval is %v, and has to be positive", t.MaxInterval)
	}
	if t.NearDuplicateBits < 0 || t.NearDuplicateBits > 64 {
		return fmt.Errorf("liveness: IntegrityThresholds.NearDuplicateBits is %d, and has to be between 0 and 64", t.NearDuplicateBits)
	}
	if t.MaxNearDuplicateRatio > 1 {
		return fmt.Errorf("liveness: IntegrityThresholds.MaxNearDuplicateRatio is %v, and has to be at most 1", t.MaxNearDuplicateRatio)
	}
	if !finite(t.ExpectedAspect) || t.ExpectedAspect <= 0 {
		return fmt.Errorf("liveness: IntegrityThresholds.ExpectedAspect is %v, and has to be positive", t.ExpectedAspect)
	}
	return nil
}

// CaptureFrame is one frame of a capture as the client sent it.
type CaptureFrame struct {
	// Timestamp is when the frame was taken, in the server's time base.
	Timestamp time.Time
	// Image is the decoded frame. When it is nil, Encoded is decoded with
	// arandu-onnx's DecodeImage.
	Image image.Image
	// Encoded is the frame's bytes as received, when the client sent an
	// encoded image; its JPEG markers are read for MetadataSignal. It may be
	// nil.
	Encoded []byte
}

// Capture is a sequence of frames and the nonce binding of its metadata.
type Capture struct {
	// Frames are the frames, in the order they were taken.
	Frames []CaptureFrame
	// ExpectedNonce is the nonce the server issued for the capture.
	ExpectedNonce string
	// EchoedNonce is the nonce the client put in the capture metadata.
	EchoedNonce string
}

// TimingSignal is the regularity of the frames' timestamps.
type TimingSignal struct {
	// Intervals is how many intervals there are between frames.
	Intervals int
	// MeanInterval and StdInterval are the mean and the population standard
	// deviation of the intervals.
	MeanInterval, StdInterval time.Duration
	// MinInterval and MaxInterval are the shortest and the longest.
	MinInterval, MaxInterval time.Duration
	// JitterCV is StdInterval over MeanInterval, 0 when the mean is not
	// positive.
	JitterCV float64
	// NonIncreasing is how many frames are not later than the frame before.
	NonIncreasing int
}

// DuplicateSignal is how alike the frames are.
type DuplicateSignal struct {
	// Hashes are each frame's DifferenceHash, in order.
	Hashes []uint64
	// ExactDuplicatePairs is how many pairs of frames, anywhere in the
	// capture, hold identical pixels.
	ExactDuplicatePairs int
	// NearDuplicatePairs is how many pairs of frames, anywhere in the
	// capture, are within the near-duplicate distance.
	NearDuplicatePairs int
	// ConsecutiveNearDuplicates is how many consecutive pairs are.
	ConsecutiveNearDuplicates int
	// ConsecutiveNearDuplicateRatio is ConsecutiveNearDuplicates over the
	// consecutive pairs.
	ConsecutiveNearDuplicateRatio float64
	// MeanConsecutiveDistance is the mean hash distance, in bits, between
	// consecutive frames.
	MeanConsecutiveDistance float64
}

// GeometrySignal is the frames' sizes.
type GeometrySignal struct {
	// Sizes are the distinct frame sizes, in order of first appearance.
	Sizes []image.Point
	// SizeChanges is how many frames differ in size from the one before.
	SizeChanges int
	// Aspect is the first frame's shape against the expected one.
	Aspect CaptureAspectSignal
	// FramesOffAspect is how many frames are farther from the expected
	// aspect than the tolerance.
	FramesOffAspect int
}

// MetadataSignal is what the encoded frames' markers say, for the frames
// that came encoded.
type MetadataSignal struct {
	// Frames are each encoded frame's markers, in order.
	Frames []JPEGMetadata
	// Encoded is how many frames came with their encoded bytes.
	Encoded int
	// WithEXIF is how many of those carry an EXIF segment.
	WithEXIF int
	// MixedEXIF is whether some encoded frames carry EXIF and some do not.
	MixedEXIF bool
	// EditingSegments is how many encoded frames carry a Photoshop (APP13)
	// or an XMP segment, which a camera capture does not write.
	EditingSegments int
	// Software are the distinct EXIF Software values.
	Software []string
	// HeaderSizeDifferences is how many encoded frames declare a size in their
	// frame header other than the size they decode to, either way round.
	HeaderSizeDifferences int
}

// InjectionSignals is what MeasureInjectionSignals returns: signals of a
// replayed, injected or edited capture, each with its reference. They are
// signals for the application's policy, never a conclusion: every one of them
// has innocent causes, and none of them sees a capture that was injected
// well.
type InjectionSignals struct {
	// FrameCount is how many frames were read.
	FrameCount int
	// Nonce is whether the capture metadata echoed the issued nonce.
	Nonce NonceBinding
	// Timing is the regularity of the timestamps.
	Timing TimingSignal
	// Duplicates is how alike the frames are.
	Duplicates DuplicateSignal
	// Geometry is the frames' sizes.
	Geometry GeometrySignal
	// Metadata is what the encoded frames' markers say.
	Metadata MetadataSignal
	// Checks hold each signal against its reference, in a fixed order.
	Checks []Check
	// Thresholds are the references the checks used, with their version.
	Thresholds IntegrityThresholds
}

// MeasureInjectionSignals measures, in pure Go, what a capture's frames and
// metadata show about how it was produced: the nonce binding, the timing
// regularity, duplicated or near-duplicated frames, changes of resolution
// and aspect, and the EXIF and editing markers of encoded frames. Each signal
// is checked against the references. Nothing runs a model, and nothing here
// concludes a capture was injected.
func MeasureInjectionSignals(capture Capture, thresholds IntegrityThresholds) (InjectionSignals, error) {
	if err := thresholds.Validate(); err != nil {
		return InjectionSignals{}, err
	}
	if len(capture.Frames) == 0 {
		return InjectionSignals{}, errors.New("liveness: MeasureInjectionSignals of a capture with no frames")
	}
	if len(capture.Frames) > MaxFrames {
		return InjectionSignals{}, fmt.Errorf("liveness: %d frames is more than %d", len(capture.Frames), MaxFrames)
	}
	s := InjectionSignals{
		FrameCount: len(capture.Frames),
		Nonce:      BindNonce(capture.ExpectedNonce, capture.EchoedNonce),
		Thresholds: thresholds,
	}
	images := make([]image.Image, len(capture.Frames))
	for i, f := range capture.Frames {
		img := f.Image
		if img == nil {
			if f.Encoded == nil {
				return InjectionSignals{}, fmt.Errorf("liveness: frame %d has neither an image nor encoded bytes", i)
			}
			decoded, err := onnx.DecodeImage(bytes.NewReader(f.Encoded))
			if err != nil {
				return InjectionSignals{}, fmt.Errorf("liveness: frame %d: %w", i, err)
			}
			img = decoded
		}
		b := img.Bounds()
		if b.Empty() {
			return InjectionSignals{}, fmt.Errorf("liveness: frame %d is empty", i)
		}
		if int64(b.Dx())*int64(b.Dy()) > onnx.MaxImagePixels {
			return InjectionSignals{}, fmt.Errorf("%w: frame %d is %dx%d", onnx.ErrImageTooLarge, i, b.Dx(), b.Dy())
		}
		images[i] = img
	}

	times := make([]time.Time, len(capture.Frames))
	for i, f := range capture.Frames {
		times[i] = f.Timestamp
	}
	s.Timing = FrameTiming(times)
	s.Duplicates = duplicates(images, thresholds.NearDuplicateBits)
	s.Geometry = geometry(images, thresholds)
	s.Metadata = metadata(capture.Frames, images)

	s.Checks = []Check{
		isTrue("NonceEchoed", s.Nonce.Equal),
		atMost("NonIncreasingTimestamps", float64(s.Timing.NonIncreasing), 0),
	}
	if s.Timing.Intervals > 0 {
		s.Checks = append(s.Checks,
			between("JitterCV", s.Timing.JitterCV, thresholds.MinJitterCV, thresholds.MaxJitterCV),
			atMost("MaxIntervalSeconds", s.Timing.MaxInterval.Seconds(), thresholds.MaxInterval.Seconds()),
			atMost("ExactDuplicatePairs", float64(s.Duplicates.ExactDuplicatePairs), 0),
			atMost("ConsecutiveNearDuplicateRatio", s.Duplicates.ConsecutiveNearDuplicateRatio, thresholds.MaxNearDuplicateRatio),
		)
	}
	s.Checks = append(s.Checks,
		atMost("SizeChanges", float64(s.Geometry.SizeChanges), 0),
		atMost("FramesOffAspect", float64(s.Geometry.FramesOffAspect), 0),
	)
	if s.Metadata.Encoded > 0 {
		s.Checks = append(s.Checks,
			isFalse("MixedEXIF", s.Metadata.MixedEXIF),
			atMost("EditingSegments", float64(s.Metadata.EditingSegments), 0),
			atMost("HeaderSizeDifferences", float64(s.Metadata.HeaderSizeDifferences), 0),
		)
	}
	return s, nil
}

// MeasureInjectionSignals is the package's MeasureInjectionSignals with the
// configured thresholds. It needs no boot.
func (m *Module) MeasureInjectionSignals(capture Capture) (InjectionSignals, error) {
	return MeasureInjectionSignals(capture, m.cfg.Integrity)
}

// FrameTiming measures the intervals between consecutive timestamps. A
// non-increasing step is counted, and its interval, zero or negative, enters
// the statistics as it is.
func FrameTiming(timestamps []time.Time) TimingSignal {
	var t TimingSignal
	if len(timestamps) < 2 {
		return t
	}
	intervals := make([]float64, 0, len(timestamps)-1)
	for i := 1; i < len(timestamps); i++ {
		d := timestamps[i].Sub(timestamps[i-1])
		if d <= 0 {
			t.NonIncreasing++
		}
		intervals = append(intervals, float64(d))
	}
	mean, std := meanStd(intervals)
	lo, hi := intervals[0], intervals[0]
	for _, v := range intervals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	t.Intervals = len(intervals)
	t.MeanInterval, t.StdInterval = time.Duration(math.Round(mean)), time.Duration(math.Round(std))
	t.MinInterval, t.MaxInterval = time.Duration(lo), time.Duration(hi)
	if mean > 0 {
		t.JitterCV = std / mean
	}
	return t
}

// meanStd is the mean and the population standard deviation of values, or
// zeros for none.
func meanStd(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	sq := 0.0
	for _, v := range values {
		sq += (v - mean) * (v - mean)
	}
	return mean, math.Sqrt(sq / float64(len(values)))
}

// DifferenceHash is the 64-bit difference hash of an image: its BT.601 luma
// averaged over a 9×8 grid of cells, and one bit per pair of horizontally
// adjacent cells, set when the left cell is darker than the right. Bit 63 is
// the top-left pair, row by row. Two frames of one scene differ in few bits;
// HashDistance counts them.
func DifferenceHash(img image.Image) uint64 {
	const cols, rows = 9, 8
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return 0
	}
	rgba := toRGBA(img)
	var cells [rows][cols]float64
	for r := 0; r < rows; r++ {
		y0, y1 := r*h/rows, max((r+1)*h/rows, r*h/rows+1)
		for c := 0; c < cols; c++ {
			x0, x1 := c*w/cols, max((c+1)*w/cols, c*w/cols+1)
			sum, n := 0.0, 0
			for y := y0; y < min(y1, h); y++ {
				row := rgba.Pix[y*rgba.Stride:]
				for x := x0; x < min(x1, w); x++ {
					p := row[x*4:]
					sum += 0.299*float64(p[0]) + 0.587*float64(p[1]) + 0.114*float64(p[2])
					n++
				}
			}
			if n > 0 {
				cells[r][c] = sum / float64(n)
			}
		}
	}
	var hash uint64
	for r := 0; r < rows; r++ {
		for c := 0; c < cols-1; c++ {
			hash <<= 1
			if cells[r][c] < cells[r][c+1] {
				hash |= 1
			}
		}
	}
	return hash
}

// HashDistance is the number of bits in which two hashes differ.
func HashDistance(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// toRGBA returns the image as RGBA with its origin at (0, 0), copying only
// when it is not one already.
func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Rect.Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// pixelDigest is the SHA-256 of an image's size and RGBA pixels.
func pixelDigest(img image.Image) [32]byte {
	rgba := toRGBA(img)
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d:", rgba.Rect.Dx(), rgba.Rect.Dy())
	for y := 0; y < rgba.Rect.Dy(); y++ {
		h.Write(rgba.Pix[y*rgba.Stride : y*rgba.Stride+rgba.Rect.Dx()*4])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// duplicates measures how alike the frames are.
func duplicates(images []image.Image, nearBits int) DuplicateSignal {
	var d DuplicateSignal
	digests := make([][32]byte, len(images))
	for i, img := range images {
		d.Hashes = append(d.Hashes, DifferenceHash(img))
		digests[i] = pixelDigest(img)
	}
	for i := range images {
		for j := i + 1; j < len(images); j++ {
			if digests[i] == digests[j] {
				d.ExactDuplicatePairs++
			}
			if HashDistance(d.Hashes[i], d.Hashes[j]) <= nearBits {
				d.NearDuplicatePairs++
			}
		}
	}
	if pairs := len(images) - 1; pairs > 0 {
		total := 0
		for i := 1; i < len(images); i++ {
			distance := HashDistance(d.Hashes[i-1], d.Hashes[i])
			total += distance
			if distance <= nearBits {
				d.ConsecutiveNearDuplicates++
			}
		}
		d.ConsecutiveNearDuplicateRatio = float64(d.ConsecutiveNearDuplicates) / float64(pairs)
		d.MeanConsecutiveDistance = float64(total) / float64(pairs)
	}
	return d
}

// geometry measures the frames' sizes and shapes.
func geometry(images []image.Image, t IntegrityThresholds) GeometrySignal {
	var g GeometrySignal
	seen := map[image.Point]bool{}
	for i, img := range images {
		size := img.Bounds().Size()
		if !seen[size] {
			seen[size] = true
			g.Sizes = append(g.Sizes, size)
		}
		if i > 0 && size != images[i-1].Bounds().Size() {
			g.SizeChanges++
		}
		aspect := CaptureAspect(size.X, size.Y, t.ExpectedAspect, t.AspectTolerance)
		if i == 0 {
			g.Aspect = aspect
		}
		if math.Abs(aspect.Deviation) > t.AspectTolerance {
			g.FramesOffAspect++
		}
	}
	return g
}

// metadata reads the markers of the frames that came encoded.
func metadata(frames []CaptureFrame, images []image.Image) MetadataSignal {
	var m MetadataSignal
	software := map[string]bool{}
	for i, f := range frames {
		if f.Encoded == nil {
			continue
		}
		meta := ReadJPEGMetadata(f.Encoded)
		m.Frames = append(m.Frames, meta)
		m.Encoded++
		if meta.EXIF {
			m.WithEXIF++
		}
		if meta.Photoshop || meta.XMP {
			m.EditingSegments++
		}
		if meta.Software != "" && !software[meta.Software] {
			software[meta.Software] = true
			m.Software = append(m.Software, meta.Software)
		}
		size := images[i].Bounds().Size()
		declared := image.Point{X: meta.Width, Y: meta.Height}
		if meta.JPEG && declared != size && declared != (image.Point{X: size.Y, Y: size.X}) {
			m.HeaderSizeDifferences++
		}
	}
	m.MixedEXIF = m.WithEXIF > 0 && m.WithEXIF < m.Encoded
	sort.Strings(m.Software)
	return m
}
