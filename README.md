# gosml

Go library for parsing [SML (Smart Message Language)](https://de.wikipedia.org/wiki/Smart_Message_Language) data from smart meters.

This is a fork of [mfmayer/gosml](https://github.com/mfmayer/gosml) (itself a fork of [andig/gosml](https://github.com/andig/gosml)), which is a Go port of [volkszaehler/libsml](https://github.com/volkszaehler/libsml).

## Changes in this fork

Four bug fixes for reliable operation with serial IR readers on real-world smart meters:

1. **`readChunk`: use `io.ReadFull` instead of `r.Read`** — The original code uses `bufio.Reader.Read()` which may return fewer bytes than requested on serial devices. This caused corrupt SML frames. `io.ReadFull` guarantees complete reads.

2. **`call()`: bounds check for empty OBIS codes** — Prevents an index-out-of-range panic when the OBIS code slice is fully consumed during recursive callback matching.

3. **`Read()`: panic recovery around `parseFile`** — Wraps the parser in `recover()` so that malformed SML data from a meter doesn't crash the calling application. Parse panics are converted to errors and the frame is skipped.

4. **`Read()`: `continue` on parse errors instead of `return`** — The original code aborts on the first parse error. When reading a continuous serial stream, it's better to skip bad frames and keep reading. This matches the existing behavior for `ErrSequenceTooLong` and `ErrUnrecognizedSequence`.

All changes are backwards-compatible — no exported types, functions, or signatures were modified.

## Usage

```go
import (
	"github.com/petesahatt/gosml"
)
```

```go
// Register callbacks for specific OBIS codes
err := gosml.Read(reader,
	gosml.WithObisCallback(gosml.OctetString{1, 0, 1, 8, 0}, func(entry *gosml.ListEntry) {
		// handle meter reading (e.g. 1.0.1.8.0 = energy consumed)
	}),
)
```

## Example

See [examples/emmon](https://github.com/petesahatt/gosml/tree/master/examples/emmon) and the [libsml](https://github.com/volkszaehler/libsml) documentation.

Test binaries and SML files from real-world meters: <https://github.com/devZer0/libsml-testing>

## License

MIT — see [LICENSE](LICENSE) file. Original work by [andig](https://github.com/andig) and [mfmayer](https://github.com/mfmayer).
