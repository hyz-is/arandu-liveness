package unit_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
	"time"

	liveness "github.com/hyz-is/arandu-liveness"
)

// Every frame here is drawn in Go: gradients and moving sinusoidal bands. No
// capture of a person is read.

// gradient is a w by h image whose luma rises left to right, or falls when
// falling is set.
func gradient(w, h int, falling bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(x * 255 / (w - 1))
			if falling {
				v = 255 - v
			}
			img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// bands is a w by h image of sinusoidal bands whose phase is the frame
// number: each frame of a sequence differs from the one before.
func bands(w, h, frame int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	phase := 1.3 * float64(frame)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 128 + 100*math.Sin(float64(x)*0.03+float64(y)*0.011+phase)
			img.SetRGBA(x, y, color.RGBA{R: uint8(v), G: uint8(v * 0.8), B: uint8(255 - v), A: 255})
		}
	}
	return img
}

func TestDifferenceHashGoldenValues(t *testing.T) {
	t.Parallel()

	// Rising luma: every left cell is darker than its right neighbour, so
	// all 64 bits are set. Falling luma and a flat image set none.
	if h := liveness.DifferenceHash(gradient(90, 80, false)); h != ^uint64(0) {
		t.Errorf("rising gradient hash %016x", h)
	}
	if h := liveness.DifferenceHash(gradient(90, 80, true)); h != 0 {
		t.Errorf("falling gradient hash %016x", h)
	}
	flat := image.NewRGBA(image.Rect(0, 0, 90, 80))
	for i := range flat.Pix {
		flat.Pix[i] = 90
	}
	if h := liveness.DifferenceHash(flat); h != 0 {
		t.Errorf("a flat image hash %016x", h)
	}
	if liveness.HashDistance(^uint64(0), 0) != 64 || liveness.HashDistance(0b1011, 0b0001) != 2 {
		t.Error("HashDistance")
	}
	// An image smaller than the grid still hashes, and a sub-image hashes
	// its own pixels.
	_ = liveness.DifferenceHash(gradient(3, 2, false))
	whole := gradient(180, 80, false)
	if h := liveness.DifferenceHash(whole.SubImage(image.Rect(90, 0, 180, 80))); h != ^uint64(0) {
		t.Errorf("sub-image hash %016x", h)
	}
	// A slightly brighter copy of the same frame is within a few bits.
	a, b := bands(120, 160, 0), bands(120, 160, 0)
	for i := 0; i < len(b.Pix); i += 4 {
		b.Pix[i] = uint8(min(255, int(b.Pix[i])+3))
	}
	if d := liveness.HashDistance(liveness.DifferenceHash(a), liveness.DifferenceHash(b)); d > 2 {
		t.Errorf("a brightened copy is %d bits away", d)
	}
}

func ms(v ...int) []time.Time {
	out := make([]time.Time, len(v))
	for i, m := range v {
		out[i] = issued.Add(time.Duration(m) * time.Millisecond)
	}
	return out
}

func TestFrameTimingGoldenValues(t *testing.T) {
	t.Parallel()

	// Intervals 33, 33, 34, 33 ms: mean 33.25 ms; deviations -0.25 (three
	// times) and 0.75, so the variance is (3·0.0625 + 0.5625)/4 = 0.1875 ms²
	// and the standard deviation 0.4330 ms; CV 0.4330/33.25 = 0.013023.
	got := liveness.FrameTiming(ms(0, 33, 66, 100, 133))
	if got.Intervals != 4 || got.MeanInterval != 33250*time.Microsecond || got.MinInterval != 33*time.Millisecond ||
		got.MaxInterval != 34*time.Millisecond || got.NonIncreasing != 0 {
		t.Errorf("timing %+v", got)
	}
	if math.Abs(got.JitterCV-math.Sqrt(0.1875)/33.25) > 1e-9 || math.Abs(got.StdInterval.Seconds()-math.Sqrt(0.1875)/1000) > 1e-9 {
		t.Errorf("jitter %v, std %v", got.JitterCV, got.StdInterval)
	}
	if regular := liveness.FrameTiming(ms(0, 40, 80, 120)); regular.JitterCV != 0 {
		t.Errorf("a perfectly regular stream has jitter %v", regular.JitterCV)
	}
	if back := liveness.FrameTiming(ms(0, 10, 10, 5)); back.NonIncreasing != 2 {
		t.Errorf("%d non-increasing steps, want 2", back.NonIncreasing)
	}
	if one := liveness.FrameTiming(ms(0)); one.Intervals != 0 || one.JitterCV != 0 {
		t.Errorf("one frame %+v", one)
	}
}

// liveCapture is eight different 480x640 frames, about 30 per second with a
// millisecond of jitter, carrying the issued nonce.
func liveCapture() liveness.Capture {
	stamps := ms(0, 33, 67, 100, 132, 166, 200, 233)
	c := liveness.Capture{ExpectedNonce: "n-1", EchoedNonce: "n-1"}
	for i, ts := range stamps {
		c.Frames = append(c.Frames, liveness.CaptureFrame{Timestamp: ts, Image: bands(480, 640, i)})
	}
	return c
}

func signalCheck(t *testing.T, s liveness.InjectionSignals, signal string) liveness.Check {
	t.Helper()
	for _, c := range s.Checks {
		if c.Signal == signal {
			return c
		}
	}
	t.Fatalf("no %s check in %+v", signal, s.Checks)
	return liveness.Check{}
}

func measure(t *testing.T, c liveness.Capture) liveness.InjectionSignals {
	t.Helper()
	s, err := liveness.MeasureInjectionSignals(c, liveness.DefaultIntegrityThresholds())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestADistinctJitteryCaptureIsWithinEveryReference(t *testing.T) {
	t.Parallel()

	s := measure(t, liveCapture())
	for _, c := range s.Checks {
		if !c.WithinReference {
			t.Errorf("%s = %v is outside %s", c.Signal, c.Value, c.Rule)
		}
	}
	if s.FrameCount != 8 || len(s.Duplicates.Hashes) != 8 || s.Duplicates.ExactDuplicatePairs != 0 ||
		len(s.Geometry.Sizes) != 1 || s.Geometry.Aspect.Ratio != 0.75 || !s.Nonce.Equal {
		t.Errorf("signals %+v", s)
	}
	if s.Thresholds.Version != "kuaa-liveness-integrity-0.1-uncalibrated" || s.Metadata.Encoded != 0 {
		t.Errorf("thresholds %s, encoded %d", s.Thresholds.Version, s.Metadata.Encoded)
	}
}

func TestAReplayedFrameIsCountedAsDuplicates(t *testing.T) {
	t.Parallel()

	c := liveCapture()
	still := bands(480, 640, 0)
	for i := range c.Frames {
		c.Frames[i].Image = still
	}
	s := measure(t, c)
	// Eight identical frames: 8·7/2 = 28 pairs, seven consecutive.
	if s.Duplicates.ExactDuplicatePairs != 28 || s.Duplicates.NearDuplicatePairs != 28 ||
		s.Duplicates.ConsecutiveNearDuplicates != 7 || s.Duplicates.ConsecutiveNearDuplicateRatio != 1 ||
		s.Duplicates.MeanConsecutiveDistance != 0 {
		t.Errorf("duplicates %+v", s.Duplicates)
	}
	if signalCheck(t, s, "ExactDuplicatePairs").WithinReference || signalCheck(t, s, "ConsecutiveNearDuplicateRatio").WithinReference {
		t.Error("a replayed frame is within the duplicate references")
	}

	// A loop: frame 5 repeats frame 1, far from each other in time.
	loop := liveCapture()
	loop.Frames[5].Image = loop.Frames[1].Image
	if l := measure(t, loop); l.Duplicates.ExactDuplicatePairs != 1 || l.Duplicates.ConsecutiveNearDuplicates != 0 {
		t.Errorf("a looped frame %+v", l.Duplicates)
	}
}

func TestTimingGeometryAndNonceSignals(t *testing.T) {
	t.Parallel()

	regular := liveCapture()
	for i := range regular.Frames {
		regular.Frames[i].Timestamp = issued.Add(time.Duration(i) * 33 * time.Millisecond)
	}
	if c := signalCheck(t, measure(t, regular), "JitterCV"); c.WithinReference || c.Value != 0 || c.Rule != "in [0.002, 0.5]" {
		t.Errorf("a perfectly regular stream: %+v", c)
	}

	gap := liveCapture()
	gap.Frames[7].Timestamp = gap.Frames[6].Timestamp.Add(400 * time.Millisecond)
	if c := signalCheck(t, measure(t, gap), "MaxIntervalSeconds"); c.WithinReference || math.Abs(c.Value-0.4) > 1e-9 {
		t.Errorf("a 400 ms gap: %+v", c)
	}

	resized := liveCapture()
	resized.Frames[4].Image = bands(640, 480, 4)
	s := measure(t, resized)
	if s.Geometry.SizeChanges != 2 || s.Geometry.FramesOffAspect != 1 || len(s.Geometry.Sizes) != 2 ||
		signalCheck(t, s, "SizeChanges").WithinReference || signalCheck(t, s, "FramesOffAspect").WithinReference {
		t.Errorf("a frame of another size: %+v", s.Geometry)
	}

	landscape := liveCapture()
	for i := range landscape.Frames {
		landscape.Frames[i].Image = bands(640, 480, i)
	}
	if l := measure(t, landscape); l.Geometry.FramesOffAspect != 8 || l.Geometry.SizeChanges != 0 {
		t.Errorf("a 4:3 capture: %+v", l.Geometry)
	}

	wrong := liveCapture()
	wrong.EchoedNonce = "n-2"
	if n := measure(t, wrong); n.Nonce.Equal || signalCheck(t, n, "NonceEchoed").WithinReference {
		t.Errorf("a wrong nonce: %+v", n.Nonce)
	}
}

func TestMeasureInjectionSignalsRefusesWhatCannotBeMeasured(t *testing.T) {
	t.Parallel()

	th := liveness.DefaultIntegrityThresholds()
	if _, err := liveness.MeasureInjectionSignals(liveness.Capture{}, th); err == nil {
		t.Error("a capture with no frames was accepted")
	}
	if _, err := liveness.MeasureInjectionSignals(liveness.Capture{Frames: []liveness.CaptureFrame{{Timestamp: issued}}}, th); err == nil {
		t.Error("a frame with neither an image nor bytes was accepted")
	}
	if _, err := liveness.MeasureInjectionSignals(liveness.Capture{Frames: []liveness.CaptureFrame{{Encoded: []byte("not an image")}}}, th); err == nil {
		t.Error("undecodable bytes were accepted")
	}
	if _, err := liveness.MeasureInjectionSignals(liveCapture(), liveness.IntegrityThresholds{}); err == nil {
		t.Error("thresholds with no version were accepted")
	}
	many := liveness.Capture{Frames: make([]liveness.CaptureFrame, liveness.MaxFrames+1)}
	if _, err := liveness.MeasureInjectionSignals(many, th); err == nil {
		t.Error("more than MaxFrames frames were accepted")
	}

	// One frame has no interval: the timing and duplicate checks are left
	// out rather than reported as within their references.
	single := liveCapture()
	single.Frames = single.Frames[:1]
	s := measure(t, single)
	for _, c := range s.Checks {
		if c.Signal == "JitterCV" || c.Signal == "ExactDuplicatePairs" {
			t.Errorf("a single frame carries the %s check", c.Signal)
		}
	}
}

// encode returns a JPEG of the image, with the segments given inserted right
// after the start-of-image marker.
func encode(t *testing.T, img image.Image, segments ...[]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	out := append([]byte{}, data[:2]...)
	for _, s := range segments {
		out = append(out, s...)
	}
	return append(out, data[2:]...)
}

// segment builds a marker segment: 0xFF, the marker, the big-endian length
// including itself, and the payload.
func segment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}

// exifSegment is an APP1 Exif segment whose IFD0 holds Make, Model, Software
// and Orientation, in the byte order given.
func exifSegment(order binary.ByteOrder, mark string, software string) []byte {
	make_, model, soft := "SynthCam\x00", "M1\x00", software+"\x00"
	const entries = 4
	dataAt := 8 + 2 + 12*entries + 4
	tiff := make([]byte, dataAt)
	copy(tiff, mark)
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], entries)
	entry := func(i int, tag, kind uint16, count, value uint32) {
		at := 10 + 12*i
		order.PutUint16(tiff[at:], tag)
		order.PutUint16(tiff[at+2:], kind)
		order.PutUint32(tiff[at+4:], count)
		order.PutUint32(tiff[at+8:], value)
	}
	entry(0, 0x010F, 2, uint32(len(make_)), uint32(dataAt))
	// Three bytes fit in the entry itself.
	entry(1, 0x0110, 2, uint32(len(model)), 0)
	copy(tiff[10+12+8:], model)
	entry(2, 0x0112, 3, 1, 0)
	order.PutUint16(tiff[10+24+8:], 6)
	entry(3, 0x0131, 2, uint32(len(soft)), uint32(dataAt+len(make_)))
	tiff = append(tiff, make_...)
	tiff = append(tiff, soft...)
	return segment(0xE1, append([]byte("Exif\x00\x00"), tiff...))
}

func TestReadJPEGMetadataReadsTheMarkers(t *testing.T) {
	t.Parallel()

	img := bands(48, 64, 0)
	plain := liveness.ReadJPEGMetadata(encode(t, img))
	if !plain.JPEG || plain.EXIF || plain.Width != 48 || plain.Height != 64 || plain.Progressive || plain.Truncated {
		t.Errorf("a plain JPEG %+v", plain)
	}
	if last := plain.Markers[len(plain.Markers)-1]; last != "SOS" {
		t.Errorf("markers %v do not end at the scan", plain.Markers)
	}

	for _, tc := range []struct {
		order binary.ByteOrder
		mark  string
	}{{binary.LittleEndian, "II"}, {binary.BigEndian, "MM"}} {
		m := liveness.ReadJPEGMetadata(encode(t, img, exifSegment(tc.order, tc.mark, "Synth Editor 2.0")))
		if !m.EXIF || m.Make != "SynthCam" || m.Model != "M1" || m.Software != "Synth Editor 2.0" || m.Orientation != 6 || m.Truncated {
			t.Errorf("%s: %+v", tc.mark, m)
		}
		if m.Markers[0] != "APP1" {
			t.Errorf("%s: markers %v", tc.mark, m.Markers)
		}
	}

	edited := liveness.ReadJPEGMetadata(encode(t, img,
		segment(0xE0, []byte("JFIF\x00\x01\x02\x00\x00\x01\x00\x01\x00\x00")),
		segment(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<x/>")),
		segment(0xE2, []byte("ICC_PROFILE\x00\x01\x01")),
		segment(0xED, []byte("Photoshop 3.0\x008BIM")),
	))
	if !edited.JFIF || !edited.XMP || !edited.ICC || !edited.Photoshop {
		t.Errorf("an edited JPEG %+v", edited)
	}

	if m := liveness.ReadJPEGMetadata([]byte("GIF89a")); m.JPEG {
		t.Error("a GIF was read as a JPEG")
	}
	full := encode(t, img, exifSegment(binary.LittleEndian, "II", "x"))
	if m := liveness.ReadJPEGMetadata(full[:30]); !m.JPEG || !m.Truncated {
		t.Errorf("a cut JPEG %+v", m)
	}
}

func TestEncodedFramesCarryMetadataSignals(t *testing.T) {
	t.Parallel()

	c := liveCapture()
	for i := range c.Frames {
		img := c.Frames[i].Image
		c.Frames[i].Image = nil
		if i%2 == 0 {
			c.Frames[i].Encoded = encode(t, img, exifSegment(binary.LittleEndian, "II", "SynthCam Firmware"))
		} else {
			c.Frames[i].Encoded = encode(t, img)
		}
	}
	s := measure(t, c)
	if s.Metadata.Encoded != 8 || s.Metadata.WithEXIF != 4 || !s.Metadata.MixedEXIF || len(s.Metadata.Software) != 1 ||
		s.Metadata.Software[0] != "SynthCam Firmware" || s.Metadata.HeaderSizeDifferences != 0 {
		t.Errorf("metadata %+v", s.Metadata)
	}
	if signalCheck(t, s, "MixedEXIF").WithinReference || !signalCheck(t, s, "EditingSegments").WithinReference {
		t.Errorf("checks %+v", s.Checks)
	}
	// The EXIF orientation 6 turns the decoded frames: 640x480, the
	// declared header size either way round, so no mismatch.
	if s.Geometry.Sizes[0] != (image.Point{X: 640, Y: 480}) || s.Geometry.SizeChanges != 7 {
		t.Errorf("geometry %+v", s.Geometry)
	}

	edited := liveCapture()
	edited.Frames[0].Encoded = encode(t, edited.Frames[0].Image, segment(0xED, []byte("Photoshop 3.0\x008BIM")))
	e := measure(t, edited)
	if e.Metadata.Encoded != 1 || e.Metadata.EditingSegments != 1 || signalCheck(t, e, "EditingSegments").WithinReference {
		t.Errorf("an edited frame %+v", e.Metadata)
	}
	mismatch := liveCapture()
	mismatch.Frames[0].Encoded = encode(t, bands(100, 100, 0))
	if m := measure(t, mismatch); m.Metadata.HeaderSizeDifferences != 1 {
		t.Errorf("encoded bytes of another size %+v", m.Metadata)
	}
}
