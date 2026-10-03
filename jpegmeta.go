package liveness

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// maxMarkers is the most segment names JPEGMetadata keeps.
const maxMarkers = 64

// maxTagText is the longest EXIF text value JPEGMetadata keeps.
const maxTagText = 128

// JPEGMetadata is what the markers of a JPEG file say, read up to the start
// of its entropy-coded data. Nothing is decoded and nothing is trusted: every
// value is what the file claims.
type JPEGMetadata struct {
	// JPEG is whether the bytes start with a JPEG start-of-image marker.
	JPEG bool
	// Markers are the names of the segments before the scan, in order
	// ("APP0", "APP1", "DQT", "SOF0", …), at most 64.
	Markers []string
	// Width and Height are the frame header's size, 0 without one.
	Width, Height int
	// Progressive is whether the frame header is a progressive one.
	Progressive bool
	// JFIF is whether an APP0 JFIF segment is present.
	JFIF bool
	// EXIF is whether an APP1 Exif segment is present.
	EXIF bool
	// XMP is whether an APP1 XMP segment is present.
	XMP bool
	// ICC is whether an APP2 ICC profile is present.
	ICC bool
	// Photoshop is whether an APP13 Photoshop segment is present.
	Photoshop bool
	// Make, Model, Software and DateTime are the EXIF IFD0 text tags of those
	// names, printable ASCII only and cut at 128 characters.
	Make, Model, Software, DateTime string
	// Orientation is the EXIF orientation tag, 0 without one.
	Orientation int
	// Truncated is whether a segment ran past the end of the bytes.
	Truncated bool
}

// ReadJPEGMetadata reads the markers of a JPEG file up to its first scan. It
// never fails: bytes that are not a JPEG give JPEG false, and a segment cut
// short sets Truncated.
func ReadJPEGMetadata(data []byte) JPEGMetadata {
	var m JPEGMetadata
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		return m
	}
	m.JPEG = true
	i := 2
	for i < len(data) {
		if data[i] != 0xFF {
			// Not at a marker: the stream is malformed from here.
			m.Truncated = true
			return m
		}
		for i < len(data) && data[i] == 0xFF {
			i++
		}
		if i >= len(data) {
			m.Truncated = true
			return m
		}
		marker := data[i]
		i++
		switch {
		case marker == 0xD9 || marker == 0xDA:
			m.addMarker(markerName(marker))
			return m
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD8):
			continue
		}
		if i+2 > len(data) {
			m.Truncated = true
			return m
		}
		length := int(binary.BigEndian.Uint16(data[i:]))
		if length < 2 || i+length > len(data) {
			m.Truncated = true
			return m
		}
		payload := data[i+2 : i+length]
		i += length
		m.addMarker(markerName(marker))
		m.readSegment(marker, payload)
	}
	return m
}

// addMarker records a segment name while there is room.
func (m *JPEGMetadata) addMarker(name string) {
	if len(m.Markers) < maxMarkers {
		m.Markers = append(m.Markers, name)
	}
}

// readSegment reads what one segment says.
func (m *JPEGMetadata) readSegment(marker byte, payload []byte) {
	switch {
	case marker == 0xE0 && bytes.HasPrefix(payload, []byte("JFIF\x00")):
		m.JFIF = true
	case marker == 0xE1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")):
		m.EXIF = true
		m.readTIFF(payload[6:])
	case marker == 0xE1 && bytes.HasPrefix(payload, []byte("http://ns.adobe.com/xap/1.0/\x00")):
		m.XMP = true
	case marker == 0xE2 && bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")):
		m.ICC = true
	case marker == 0xED && bytes.HasPrefix(payload, []byte("Photoshop 3.0\x00")):
		m.Photoshop = true
	case isFrameHeader(marker) && len(payload) >= 5:
		m.Height = int(binary.BigEndian.Uint16(payload[1:]))
		m.Width = int(binary.BigEndian.Uint16(payload[3:]))
		m.Progressive = marker == 0xC2 || marker == 0xC6 || marker == 0xCA || marker == 0xCE
	}
}

// isFrameHeader reports whether a marker is a start-of-frame one: C0 to CF
// except DHT (C4), JPG (C8) and DAC (CC).
func isFrameHeader(marker byte) bool {
	return marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC
}

// markerName is a marker's conventional name.
func markerName(marker byte) string {
	switch {
	case marker >= 0xE0 && marker <= 0xEF:
		return fmt.Sprintf("APP%d", marker-0xE0)
	case marker == 0xC4:
		return "DHT"
	case marker == 0xCC:
		return "DAC"
	case isFrameHeader(marker):
		return fmt.Sprintf("SOF%d", marker-0xC0)
	case marker == 0xDB:
		return "DQT"
	case marker == 0xDD:
		return "DRI"
	case marker == 0xDA:
		return "SOS"
	case marker == 0xD9:
		return "EOI"
	case marker == 0xFE:
		return "COM"
	}
	return fmt.Sprintf("0x%02X", marker)
}

// readTIFF reads the text tags and the orientation of an EXIF block's IFD0.
func (m *JPEGMetadata) readTIFF(tiff []byte) {
	if len(tiff) < 8 {
		m.Truncated = true
		return
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return
	}
	if order.Uint16(tiff[2:]) != 42 {
		return
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		m.Truncated = true
		return
	}
	count := int(order.Uint16(tiff[ifd:]))
	for e := 0; e < count; e++ {
		at := ifd + 2 + 12*e
		if at+12 > len(tiff) {
			m.Truncated = true
			return
		}
		tag, kind, n := order.Uint16(tiff[at:]), order.Uint16(tiff[at+2:]), int(order.Uint32(tiff[at+4:]))
		switch tag {
		case 0x0112:
			if kind == 3 && n >= 1 {
				m.Orientation = int(order.Uint16(tiff[at+8:]))
			}
		case 0x010F, 0x0110, 0x0131, 0x0132:
			if kind != 2 {
				continue
			}
			text, ok := asciiValue(tiff, order, at, n)
			if !ok {
				m.Truncated = true
				continue
			}
			switch tag {
			case 0x010F:
				m.Make = text
			case 0x0110:
				m.Model = text
			case 0x0131:
				m.Software = text
			case 0x0132:
				m.DateTime = text
			}
		}
	}
}

// asciiValue reads an ASCII entry's value: inline when it fits in four
// bytes, at its offset otherwise.
func asciiValue(tiff []byte, order binary.ByteOrder, entry, n int) (string, bool) {
	var raw []byte
	if n <= 4 {
		raw = tiff[entry+8 : entry+8+n]
	} else {
		offset := int(order.Uint32(tiff[entry+8:]))
		if offset < 0 || n < 0 || offset+n > len(tiff) {
			return "", false
		}
		raw = tiff[offset : offset+n]
	}
	var b strings.Builder
	for _, c := range raw {
		if c == 0 {
			break
		}
		if c >= 0x20 && c < 0x7F && b.Len() < maxTagText {
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String()), true
}
