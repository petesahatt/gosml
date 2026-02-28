package gosml

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests: readChunk
// ---------------------------------------------------------------------------

func TestReadChunk_Full(t *testing.T) {
	// Simulate a reader that delivers data in small pieces (1 byte at a time).
	// io.ReadFull must still fill the entire buffer.
	data := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	r := bufio.NewReaderSize(iotest_oneByteReader(bytes.NewReader(data)), 1)
	buf := make([]byte, 4)
	if err := readChunk(r, buf); err != nil {
		t.Fatalf("readChunk returned error: %v", err)
	}
	if !bytes.Equal(buf, data) {
		t.Fatalf("expected %x, got %x", data, buf)
	}
}

func TestReadChunk_EOF(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader(nil))
	buf := make([]byte, 4)
	err := readChunk(r, buf)
	if err == nil {
		t.Fatal("expected error on empty reader")
	}
}

// oneByteReader wraps a reader and returns at most 1 byte per Read call,
// simulating a slow serial device.
type oneByteReaderImpl struct {
	r io.Reader
}

func iotest_oneByteReader(r io.Reader) io.Reader {
	return &oneByteReaderImpl{r: r}
}

func (o *oneByteReaderImpl) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}

// ---------------------------------------------------------------------------
// Unit tests: OBIS callback bounds check
// ---------------------------------------------------------------------------

func TestCallBoundsCheck(t *testing.T) {
	// Calling with an empty ObjName must not panic.
	oc := newObisGroupCallback()
	called := false
	oc.addCallback(OctetString{1, 0}, func(le *ListEntry) {
		called = true
	})
	entry := &ListEntry{ObjName: OctetString{}} // empty
	oc.call(entry.ObjName, entry)
	if called {
		t.Fatal("callback should not have been called for empty OBIS code")
	}
}

// ---------------------------------------------------------------------------
// Unit tests: parseFile panic recovery
// ---------------------------------------------------------------------------

func TestParseFilePanicRecovery(t *testing.T) {
	// Corrupt data that will cause a panic inside parseFile.
	// The Read() function wraps parseFile in a recover(), so it should
	// return nil error (it skips bad frames and continues).
	corrupt := buildSMLFrame([]byte{0x76, 0xFF, 0xFF, 0xFF})
	r := bufio.NewReader(bytes.NewReader(corrupt))
	err := Read(r)
	if err != nil {
		t.Fatalf("Read should not return error on corrupt frame, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: Read continues on parse error
// ---------------------------------------------------------------------------

func TestReadContinuesOnParseError(t *testing.T) {
	// Build two frames: first one corrupt, second one valid (from DZG fixture).
	corrupt := buildSMLFrame([]byte{0x76, 0xFF, 0xFF, 0xFF})

	dzgData, err := os.ReadFile("testdata/DZG_DVS-7412.2_jmberg.bin")
	if err != nil {
		t.Skipf("test fixture not available: %v", err)
	}

	combined := append(corrupt, dzgData...)
	r := bufio.NewReader(bytes.NewReader(combined))

	var count int
	err = Read(r, WithObisCallback(OctetString{1, 0, 1, 8, 0}, func(le *ListEntry) {
		count++
	}))
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if count == 0 {
		t.Fatal("expected callback to be called at least once after skipping corrupt frame")
	}
}

// ---------------------------------------------------------------------------
// Unit tests: CRC16
// ---------------------------------------------------------------------------

func TestCRC16KnownValue(t *testing.T) {
	// CRC-16/X-25 (same polynomial as DIN EN 62056-46) with byte-swap.
	// Lock known values to detect accidental changes to the CRC implementation.
	data := []byte("123456789")
	crc := crc16Calculate(data, len(data))
	// Snapshot: the CRC of "123456789" must always be this value
	const expected uint16 = 0x6e90
	if crc != expected {
		t.Fatalf("CRC mismatch: got 0x%04x, expected 0x%04x", crc, expected)
	}

	// Determinism
	crc2 := crc16Calculate(data, len(data))
	if crc != crc2 {
		t.Fatalf("CRC not deterministic: %04x vs %04x", crc, crc2)
	}

	// Single byte
	crcSingle := crc16Calculate([]byte{0x00}, 1)
	const expectedSingle uint16 = 0x78f0
	if crcSingle != expectedSingle {
		t.Fatalf("CRC single byte: got 0x%04x, expected 0x%04x", crcSingle, expectedSingle)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: Buffer parsing
// ---------------------------------------------------------------------------

func TestU8Parse(t *testing.T) {
	// TL byte: type=unsigned(0x60), length=2 (1 byte data + 1 TL byte)
	// Data: 0x42
	buf := &Buffer{Bytes: []byte{0x62, 0x42}, Cursor: 0}
	val, err := buf.U8Parse()
	if err != nil {
		t.Fatalf("U8Parse error: %v", err)
	}
	if val != 0x42 {
		t.Fatalf("expected 0x42, got 0x%02x", val)
	}
}

func TestU16Parse(t *testing.T) {
	// TL: type=unsigned(0x60), length=3 (2 bytes data + 1 TL)
	buf := &Buffer{Bytes: []byte{0x63, 0x01, 0x00}, Cursor: 0}
	val, err := buf.U16Parse()
	if err != nil {
		t.Fatalf("U16Parse error: %v", err)
	}
	if val != 256 {
		t.Fatalf("expected 256, got %d", val)
	}
}

func TestU32Parse(t *testing.T) {
	// TL: type=unsigned(0x60), length=5 (4 bytes data + 1 TL)
	buf := &Buffer{Bytes: []byte{0x65, 0x00, 0x01, 0x00, 0x00}, Cursor: 0}
	val, err := buf.U32Parse()
	if err != nil {
		t.Fatalf("U32Parse error: %v", err)
	}
	if val != 65536 {
		t.Fatalf("expected 65536, got %d", val)
	}
}

func TestI8Parse(t *testing.T) {
	// TL: type=integer(0x50), length=2
	// Data: 0xFE = -2 as int8
	buf := &Buffer{Bytes: []byte{0x52, 0xFE}, Cursor: 0}
	val, err := buf.I8Parse()
	if err != nil {
		t.Fatalf("I8Parse error: %v", err)
	}
	if val != -2 {
		t.Fatalf("expected -2, got %d", val)
	}
}

func TestI64Parse(t *testing.T) {
	// TL: type=integer(0x50), length=9 (8 bytes data + 1 TL)
	buf := &Buffer{Bytes: []byte{0x59, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, Cursor: 0}
	val, err := buf.I64Parse()
	if err != nil {
		t.Fatalf("I64Parse error: %v", err)
	}
	if val != -1 {
		t.Fatalf("expected -1, got %d", val)
	}
}

func TestOctetStringParse(t *testing.T) {
	// TL: type=octet_string(0x00), length=4 (3 bytes data + 1 TL)
	buf := &Buffer{Bytes: []byte{0x04, 0x41, 0x42, 0x43}, Cursor: 0}
	val, err := buf.OctetStringParse()
	if err != nil {
		t.Fatalf("OctetStringParse error: %v", err)
	}
	if !bytes.Equal(val, []byte("ABC")) {
		t.Fatalf("expected ABC, got %x", val)
	}
}

func TestOctetStringParse_Skipped(t *testing.T) {
	buf := &Buffer{Bytes: []byte{OCTET_OPTIONAL_SKIPPED}, Cursor: 0}
	val, err := buf.OctetStringParse()
	if err != nil {
		t.Fatalf("OctetStringParse error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil for skipped optional, got %x", val)
	}
}

// ---------------------------------------------------------------------------
// Regression tests: real SML meter data
// ---------------------------------------------------------------------------

func TestReadRealMeter_DZG(t *testing.T) {
	data, err := os.ReadFile("testdata/DZG_DVS-7412.2_jmberg.bin")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}

	obisValues := map[string]int64{}
	r := bufio.NewReader(bytes.NewReader(data))
	err = Read(r,
		WithObisCallback(OctetString{1, 0, 1, 8, 0}, func(le *ListEntry) {
			obisValues["1.8.0"] = le.Value.DataInt
		}),
		WithObisCallback(OctetString{1, 0, 2, 8, 0}, func(le *ListEntry) {
			obisValues["2.8.0"] = le.Value.DataInt
		}),
		WithObisCallback(OctetString{1, 0, 16, 7, 0}, func(le *ListEntry) {
			obisValues["16.7.0"] = le.Value.DataInt
		}),
	)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	// DZG DVS-7412.2: must contain Bezug (1.8.0) and Einspeisung (2.8.0)
	if v, ok := obisValues["1.8.0"]; !ok {
		t.Fatal("missing OBIS 1-0:1.8.0 (Bezug)")
	} else if v <= 0 {
		t.Fatalf("Bezug value should be positive, got %d", v)
	}

	if v, ok := obisValues["2.8.0"]; !ok {
		t.Fatal("missing OBIS 1-0:2.8.0 (Einspeisung)")
	} else if v <= 0 {
		t.Fatalf("Einspeisung value should be positive, got %d", v)
	}

	// Leistung (16.7.0) = OBIS 1-0:16.7.0
	if _, ok := obisValues["16.7.0"]; !ok {
		t.Fatal("missing OBIS 1-0:16.7.0 (Leistung)")
	}
}

func TestReadRealMeter_EMH(t *testing.T) {
	data, err := os.ReadFile("testdata/EMH_eHZ-HW8E2A5L0EK2P.bin")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}

	var count int
	r := bufio.NewReader(bytes.NewReader(data))
	err = Read(r,
		WithObisCallback(OctetString{1, 0, 1, 8, 0}, func(le *ListEntry) {
			count++
			if le.Value.DataInt <= 0 {
				t.Errorf("EMH Bezug should be positive, got %d", le.Value.DataInt)
			}
			if le.Unit != 30 { // Wh
				t.Errorf("EMH Bezug unit should be 30 (Wh), got %d", le.Unit)
			}
		}),
	)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if count == 0 {
		t.Fatal("EMH: no Bezug values found")
	}
	// EMH file contains multiple SML frames
	if count < 5 {
		t.Fatalf("EMH: expected multiple frames with Bezug, got %d", count)
	}
}

func TestReadRealMeter_ISKRA(t *testing.T) {
	data, err := os.ReadFile("testdata/ISKRA_MT175_eHZ.bin")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}

	obisFound := map[string]bool{}
	r := bufio.NewReader(bytes.NewReader(data))
	err = Read(r,
		WithObisCallback(OctetString{1, 0, 1, 8, 0}, func(le *ListEntry) {
			obisFound["1.8.0"] = true
		}),
		WithObisCallback(OctetString{1, 0, 16, 7, 0}, func(le *ListEntry) {
			obisFound["16.7.0"] = true
		}),
		WithObisCallback(OctetString{1, 0, 36, 7, 0}, func(le *ListEntry) {
			obisFound["36.7.0"] = true // Phase L1
		}),
		WithObisCallback(OctetString{1, 0, 56, 7, 0}, func(le *ListEntry) {
			obisFound["56.7.0"] = true // Phase L2
		}),
		WithObisCallback(OctetString{1, 0, 76, 7, 0}, func(le *ListEntry) {
			obisFound["76.7.0"] = true // Phase L3
		}),
	)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	for _, code := range []string{"1.8.0", "16.7.0", "36.7.0", "56.7.0", "76.7.0"} {
		if !obisFound[code] {
			t.Errorf("ISKRA MT175: missing OBIS 1-0:%s", code)
		}
	}
}

func TestReadRealMeter_ITRON(t *testing.T) {
	data, err := os.ReadFile("testdata/ITRON_OpenWay-3.HZ.bin")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}

	var bezug int64
	r := bufio.NewReader(bytes.NewReader(data))
	err = Read(r,
		WithObisCallback(OctetString{1, 0, 1, 8, 0}, func(le *ListEntry) {
			bezug = le.Value.DataInt
		}),
	)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if bezug <= 0 {
		t.Fatalf("ITRON Bezug should be positive, got %d", bezug)
	}
}

func TestReadAllFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.bin")
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no test fixtures found")
	}

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read error: %v", err)
			}

			r := bufio.NewReader(bytes.NewReader(data))
			// Must not panic or return unexpected errors.
			// EOF and parse errors are handled gracefully by Read().
			err = Read(r)
			if err != nil {
				t.Fatalf("Read error on %s: %v", filepath.Base(f), err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildSMLFrame wraps payload in SML start/end escape sequences.
func buildSMLFrame(payload []byte) []byte {
	start := []byte{0x1b, 0x1b, 0x1b, 0x1b, 0x01, 0x01, 0x01, 0x01}
	// Pad payload to multiple of 4 bytes
	for len(payload)%4 != 0 {
		payload = append(payload, 0x00)
	}
	end := []byte{0x1b, 0x1b, 0x1b, 0x1b, 0x1a}
	padding := byte(4 - len(payload)%4)
	if padding == 4 {
		padding = 0
	}
	end = append(end, padding, 0x00, 0x00) // padding + 2 bytes CRC placeholder
	frame := append(start, payload...)
	frame = append(frame, end...)
	return frame
}
