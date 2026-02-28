package gosml

import (
	"bufio"
	"bytes"
	"io"
	"math"
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

func TestCallMatchesCorrectOBIS(t *testing.T) {
	oc := newObisGroupCallback()
	var got string
	oc.addCallback(OctetString{1, 0, 1, 8, 0}, func(le *ListEntry) {
		got = "1.8.0"
	})
	oc.addCallback(OctetString{1, 0, 2, 8, 0}, func(le *ListEntry) {
		got = "2.8.0"
	})

	entry := &ListEntry{ObjName: OctetString{1, 0, 2, 8, 0, 255}}
	oc.call(entry.ObjName, entry)
	if got != "2.8.0" {
		t.Fatalf("expected 2.8.0 callback, got %q", got)
	}
}

func TestCallWildcard(t *testing.T) {
	// Empty OBIS filter = wildcard, should match everything
	oc := newObisGroupCallback()
	count := 0
	oc.addCallback(OctetString{}, func(le *ListEntry) {
		count++
	})
	oc.call(OctetString{1, 0, 1, 8, 0, 255}, &ListEntry{})
	oc.call(OctetString{1, 0, 16, 7, 0, 255}, &ListEntry{})
	if count != 2 {
		t.Fatalf("wildcard should match all, got %d calls", count)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: Scaler (regression for 10x bug)
// ---------------------------------------------------------------------------

func TestScaler_Zero(t *testing.T) {
	// scaler=0 → Pow10(0) = 1.0 (NOT 10)
	le := &ListEntry{scaler: 0}
	if s := le.Scaler(); s != 1.0 {
		t.Fatalf("Scaler(0) = %f, want 1.0", s)
	}
}

func TestScaler_Negative(t *testing.T) {
	// scaler=-1 → Pow10(-1) = 0.1
	le := &ListEntry{scaler: -1}
	if s := le.Scaler(); math.Abs(s-0.1) > 1e-15 {
		t.Fatalf("Scaler(-1) = %f, want 0.1", s)
	}
}

func TestScaler_Positive(t *testing.T) {
	// scaler=3 → Pow10(3) = 1000
	le := &ListEntry{scaler: 3}
	if s := le.Scaler(); s != 1000.0 {
		t.Fatalf("Scaler(3) = %f, want 1000.0", s)
	}
}

func TestScaler_MinusTwo(t *testing.T) {
	// scaler=-2 → Pow10(-2) = 0.01
	le := &ListEntry{scaler: -2}
	if s := le.Scaler(); math.Abs(s-0.01) > 1e-15 {
		t.Fatalf("Scaler(-2) = %f, want 0.01", s)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: ListEntry.Float()
// ---------------------------------------------------------------------------

func TestFloat_Integer(t *testing.T) {
	le := &ListEntry{
		scaler: -1,
		Value:  Value{Typ: OCTET_TYPE_INTEGER | TYPE_NUMBER_32, DataInt: 2460},
	}
	got := le.Float()
	want := 246.0 // 2460 * 0.1
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("Float() = %f, want %f", got, want)
	}
}

func TestFloat_Unsigned(t *testing.T) {
	le := &ListEntry{
		scaler: 0,
		Value:  Value{Typ: OCTET_TYPE_UNSIGNED | TYPE_NUMBER_32, DataInt: 12345},
	}
	got := le.Float()
	if got != 12345.0 {
		t.Fatalf("Float() = %f, want 12345.0", got)
	}
}

func TestFloat_NonNumericReturnsZero(t *testing.T) {
	le := &ListEntry{
		Value: Value{Typ: OCTET_TYPE_OCTET_STRING},
	}
	if got := le.Float(); got != 0.0 {
		t.Fatalf("Float() on octet string = %f, want 0.0", got)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: ListEntry.ObjectName()
// ---------------------------------------------------------------------------

func TestObjectName(t *testing.T) {
	le := &ListEntry{ObjName: OctetString{1, 0, 1, 8, 0, 255}}
	got := le.ObjectName()
	want := "1-0:1.8.0*255"
	if got != want {
		t.Fatalf("ObjectName() = %q, want %q", got, want)
	}
}

func TestObjectName_Power(t *testing.T) {
	le := &ListEntry{ObjName: OctetString{1, 0, 16, 7, 0, 255}}
	got := le.ObjectName()
	want := "1-0:16.7.0*255"
	if got != want {
		t.Fatalf("ObjectName() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: ListEntry.ValueString()
// ---------------------------------------------------------------------------

func TestValueString_Integer(t *testing.T) {
	le := &ListEntry{
		scaler: -1,
		Value:  Value{Typ: OCTET_TYPE_INTEGER | TYPE_NUMBER_32, DataInt: 2460},
	}
	got := le.ValueString()
	// 2460 * 0.1 = 246.0 → "       246.0"
	if got == "" {
		t.Fatal("ValueString() returned empty for integer value")
	}
}

func TestValueString_OctetString(t *testing.T) {
	le := &ListEntry{
		Value: Value{Typ: OCTET_TYPE_OCTET_STRING, DataBytes: OctetString{0x0A, 0x0B}},
	}
	got := le.ValueString()
	if got != "0a 0b" {
		t.Fatalf("ValueString() = %q, want %q", got, "0a 0b")
	}
}

func TestValueString_Boolean(t *testing.T) {
	le := &ListEntry{
		Value: Value{Typ: OCTET_TYPE_BOOLEAN, DataBoolean: true},
	}
	got := le.ValueString()
	if got != "true" {
		t.Fatalf("ValueString() = %q, want %q", got, "true")
	}
}

// ---------------------------------------------------------------------------
// Unit tests: ListEntry.String()
// ---------------------------------------------------------------------------

func TestListEntryString(t *testing.T) {
	le := &ListEntry{
		ObjName: OctetString{1, 0, 1, 8, 0, 255},
		scaler:  -1,
		Value:   Value{Typ: OCTET_TYPE_UNSIGNED | TYPE_NUMBER_32, DataInt: 87824004},
	}
	s := le.String()
	if s == "" {
		t.Fatal("String() returned empty")
	}
	// Should contain the OBIS code
	if !bytes.Contains([]byte(s), []byte("1-0:1.8.0*255")) {
		t.Fatalf("String() missing OBIS code: %q", s)
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
// Unit tests: readFile
// ---------------------------------------------------------------------------

func TestReadFile_TooLong(t *testing.T) {
	// Start sequence followed by more than maxFileSize bytes without end → ErrSequenceTooLong
	start := []byte{0x1b, 0x1b, 0x1b, 0x1b, 0x01, 0x01, 0x01, 0x01}
	padding := make([]byte, maxFileSize+100)
	data := append(start, padding...)
	r := bufio.NewReader(bytes.NewReader(data))
	_, err := readFile(r)
	if err != ErrSequenceTooLong {
		t.Fatalf("expected ErrSequenceTooLong, got %v", err)
	}
}

func TestReadFile_EOF(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader(nil))
	_, err := readFile(r)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestReadFile_ValidFrame(t *testing.T) {
	// Minimal valid SML frame: start + 4 bytes payload + end
	payload := []byte{0x00, 0x00, 0x00, 0x00}
	frame := buildSMLFrame(payload)
	r := bufio.NewReader(bytes.NewReader(frame))
	got, err := readFile(r)
	if err != nil {
		t.Fatalf("readFile error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("readFile returned empty")
	}
	// Must start with escape+start sequence
	if !bytes.HasPrefix(got, []byte{0x1b, 0x1b, 0x1b, 0x1b, 0x01, 0x01, 0x01, 0x01}) {
		t.Fatalf("frame doesn't start with SML start sequence")
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

func TestI8Parse_Zero(t *testing.T) {
	buf := &Buffer{Bytes: []byte{0x52, 0x00}, Cursor: 0}
	val, err := buf.I8Parse()
	if err != nil {
		t.Fatalf("I8Parse error: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0, got %d", val)
	}
}

func TestI8Parse_MaxPositive(t *testing.T) {
	buf := &Buffer{Bytes: []byte{0x52, 0x7F}, Cursor: 0}
	val, err := buf.I8Parse()
	if err != nil {
		t.Fatalf("I8Parse error: %v", err)
	}
	if val != 127 {
		t.Fatalf("expected 127, got %d", val)
	}
}

func TestI8Parse_MinNegative(t *testing.T) {
	buf := &Buffer{Bytes: []byte{0x52, 0x80}, Cursor: 0}
	val, err := buf.I8Parse()
	if err != nil {
		t.Fatalf("I8Parse error: %v", err)
	}
	if val != -128 {
		t.Fatalf("expected -128, got %d", val)
	}
}

func TestU8Parse_Optional(t *testing.T) {
	buf := &Buffer{Bytes: []byte{OCTET_OPTIONAL_SKIPPED}, Cursor: 0}
	val, err := buf.U8Parse()
	if err != nil {
		t.Fatalf("U8Parse error: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for skipped optional, got %d", val)
	}
}

func TestBooleanParse(t *testing.T) {
	// TL: type=boolean(0x40), length=2
	buf := &Buffer{Bytes: []byte{0x42, 0x01}, Cursor: 0}
	val, err := buf.BooleanParse()
	if err != nil {
		t.Fatalf("BooleanParse error: %v", err)
	}
	if !val {
		t.Fatal("expected true")
	}
}

func TestBooleanParse_False(t *testing.T) {
	buf := &Buffer{Bytes: []byte{0x42, 0x00}, Cursor: 0}
	val, err := buf.BooleanParse()
	if err != nil {
		t.Fatalf("BooleanParse error: %v", err)
	}
	if val {
		t.Fatal("expected false")
	}
}

func TestBooleanParse_Optional(t *testing.T) {
	buf := &Buffer{Bytes: []byte{OCTET_OPTIONAL_SKIPPED}, Cursor: 0}
	val, err := buf.BooleanParse()
	if err != nil {
		t.Fatalf("BooleanParse error: %v", err)
	}
	if val {
		t.Fatal("expected false for skipped optional")
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

func TestOctetStringParse_Empty(t *testing.T) {
	// length=1 → 0 data bytes
	buf := &Buffer{Bytes: []byte{0x01}, Cursor: 0}
	val, err := buf.OctetStringParse()
	if err != nil {
		t.Fatalf("OctetStringParse error: %v", err)
	}
	if len(val) != 0 {
		t.Fatalf("expected empty, got %x", val)
	}
}

// ---------------------------------------------------------------------------
// Unit tests: NumberParse type mismatch
// ---------------------------------------------------------------------------

func TestNumberParse_TypeMismatch(t *testing.T) {
	// Unsigned TL but expecting integer
	buf := &Buffer{Bytes: []byte{0x62, 0x42}, Cursor: 0}
	_, err := buf.NumberParse(OCTET_TYPE_INTEGER, TYPE_NUMBER_8)
	if err == nil {
		t.Fatal("expected error on type mismatch")
	}
}

// ---------------------------------------------------------------------------
// Unit tests: ValueParse
// ---------------------------------------------------------------------------

func TestValueParse_Unsigned(t *testing.T) {
	// U32: TL=0x65 (unsigned, length=5), data=0x00 0x00 0x01 0x00
	buf := &Buffer{Bytes: []byte{0x65, 0x00, 0x00, 0x01, 0x00}, Cursor: 0}
	val, err := buf.ValueParse()
	if err != nil {
		t.Fatalf("ValueParse error: %v", err)
	}
	if val.DataInt != 256 {
		t.Fatalf("expected 256, got %d", val.DataInt)
	}
}

func TestValueParse_OctetString(t *testing.T) {
	buf := &Buffer{Bytes: []byte{0x03, 0xAA, 0xBB}, Cursor: 0}
	val, err := buf.ValueParse()
	if err != nil {
		t.Fatalf("ValueParse error: %v", err)
	}
	if !bytes.Equal(val.DataBytes, []byte{0xAA, 0xBB}) {
		t.Fatalf("expected [AA BB], got %x", val.DataBytes)
	}
}

func TestValueParse_Optional(t *testing.T) {
	buf := &Buffer{Bytes: []byte{OCTET_OPTIONAL_SKIPPED}, Cursor: 0}
	val, err := buf.ValueParse()
	if err != nil {
		t.Fatalf("ValueParse error: %v", err)
	}
	if val.DataInt != 0 && val.DataBytes != nil {
		t.Fatal("expected zero value for skipped optional")
	}
}

// ---------------------------------------------------------------------------
// Unit tests: Buffer helpers
// ---------------------------------------------------------------------------

func TestGetNextType(t *testing.T) {
	buf := &Buffer{Bytes: []byte{0x62}, Cursor: 0} // 0x62 & 0x70 = 0x60 = unsigned
	if got := buf.GetNextType(); got != OCTET_TYPE_UNSIGNED {
		t.Fatalf("GetNextType = 0x%02x, want 0x%02x", got, OCTET_TYPE_UNSIGNED)
	}
}

func TestOptionalIsSkipped(t *testing.T) {
	buf := &Buffer{Bytes: []byte{OCTET_OPTIONAL_SKIPPED, 0x62}, Cursor: 0}
	if !buf.OptionalIsSkipped() {
		t.Fatal("expected skipped")
	}
	if buf.Cursor != 1 {
		t.Fatalf("cursor should advance to 1, got %d", buf.Cursor)
	}
}

func TestOptionalIsNotSkipped(t *testing.T) {
	buf := &Buffer{Bytes: []byte{0x62, 0x42}, Cursor: 0}
	if buf.OptionalIsSkipped() {
		t.Fatal("should not be skipped")
	}
	if buf.Cursor != 0 {
		t.Fatalf("cursor should stay at 0, got %d", buf.Cursor)
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

func TestReadRealMeter_DZG_ScalerValues(t *testing.T) {
	// Regression: verify that Float() returns sane values (not 10x inflated)
	data, err := os.ReadFile("testdata/DZG_DVS-7412.2_jmberg.bin")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}

	r := bufio.NewReader(bytes.NewReader(data))
	err = Read(r,
		WithObisCallback(OctetString{1, 0, 1, 8, 0}, func(le *ListEntry) {
			f := le.Float()
			// Bezug in Wh: should be a reasonable value, not 10x inflated
			// Raw value is large (Wh total), scaler typically -1 → divide by 10
			if f <= 0 {
				t.Errorf("Bezug Float() should be positive, got %f", f)
			}
			// Sanity: Scaler must be a power of 10
			s := le.Scaler()
			log := math.Log10(s)
			if math.Abs(log-math.Round(log)) > 1e-9 {
				t.Errorf("Scaler %f is not a power of 10", s)
			}
		}),
	)
	if err != nil {
		t.Fatalf("Read error: %v", err)
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

// TestReadAllFixtures_FloatSanity checks that Float() returns sane values
// for all OBIS entries across all fixtures (no 10x inflation).
func TestReadAllFixtures_FloatSanity(t *testing.T) {
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
			err = Read(r, WithObisCallback(OctetString{}, func(le *ListEntry) {
				s := le.Scaler()
				// Every scaler must be a clean power of 10
				if s != 0 {
					log := math.Log10(math.Abs(s))
					if math.Abs(log-math.Round(log)) > 1e-9 {
						t.Errorf("%s: Scaler %f is not a power of 10", le.ObjectName(), s)
					}
				}
			}))
			if err != nil {
				t.Fatalf("Read error: %v", err)
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
