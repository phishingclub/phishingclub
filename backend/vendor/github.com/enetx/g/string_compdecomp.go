package g

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

type (
	// A struct that wraps a String for compression.
	compress struct{ str String }

	// A struct that wraps a String for decompression.
	decompress struct{ str String }
)

// Compress returns a compress struct wrapping the given String.
func (s String) Compress() compress { return compress{s} }

// Decompress returns a decompress struct wrapping the given String.
func (s String) Decompress() decompress { return decompress{s} }

// Zstd compresses the wrapped String using the zstd compression algorithm and
// returns the compressed data as a String.
func (c compress) Zstd() String {
	buffer := new(bytes.Buffer)
	writer, _ := zstd.NewWriter(buffer)

	_, _ = io.WriteString(writer, c.str.Std())
	_ = writer.Flush()
	_ = writer.Close()

	return String(buffer.Bytes())
}

// Zstd decompresses the wrapped String using the zstd compression algorithm and
// returns the decompressed data as a Result[String].
func (d decompress) Zstd() Result[String] {
	reader, err := zstd.NewReader(d.str.Reader())
	if err != nil {
		reader.Close()
		return Err[String](err)
	}

	defer reader.Close()

	buffer := new(bytes.Buffer)
	if _, err := io.Copy(buffer, reader); err != nil {
		return Err[String](err)
	}

	return Ok(String(buffer.Bytes()))
}

// Brotli compresses the wrapped String using the Brotli compression algorithm and
// returns the compressed data as a String.
func (c compress) Brotli() String {
	buffer := new(bytes.Buffer)
	writer := brotli.NewWriter(buffer)

	_, _ = io.WriteString(writer, c.str.Std())
	_ = writer.Flush()
	_ = writer.Close()

	return String(buffer.Bytes())
}

// Brotli decompresses the wrapped String using the Brotli compression algorithm and
// returns the decompressed data as a Result[String].
func (d decompress) Brotli() Result[String] {
	reader := brotli.NewReader(d.str.Reader())

	buffer := new(bytes.Buffer)
	if _, err := io.Copy(buffer, reader); err != nil {
		return Err[String](err)
	}

	return Ok(String(buffer.Bytes()))
}

// Zlib compresses the wrapped String using the zlib compression algorithm and
// returns the compressed data as a String.
func (c compress) Zlib() String {
	// gzcompress() php
	buffer := new(bytes.Buffer)
	writer := zlib.NewWriter(buffer)

	_, _ = io.WriteString(writer, c.str.Std())
	_ = writer.Flush()
	_ = writer.Close()

	return String(buffer.Bytes())
}

// Zlib decompresses the wrapped String using the zlib compression algorithm and
// returns the decompressed data as a Result[String].
func (d decompress) Zlib() Result[String] {
	// gzuncompress() php
	reader, err := zlib.NewReader(d.str.Reader())
	if err != nil {
		return Err[String](err)
	}

	defer reader.Close()

	buffer := new(bytes.Buffer)
	if _, err := io.Copy(buffer, reader); err != nil {
		return Err[String](err)
	}

	return Ok(String(buffer.Bytes()))
}

// Gzip compresses the wrapped String using the gzip compression format and
// returns the compressed data as a String.
func (c compress) Gzip() String {
	// gzencode() php
	buffer := new(bytes.Buffer)
	writer := gzip.NewWriter(buffer)

	_, _ = io.WriteString(writer, c.str.Std())
	_ = writer.Flush()
	_ = writer.Close()

	return String(buffer.Bytes())
}

// Gzip decompresses the wrapped String using the gzip compression format and
// returns the decompressed data as a Result[String].
func (d decompress) Gzip() Result[String] {
	// gzdecode() php
	reader, err := gzip.NewReader(d.str.Reader())
	if err != nil {
		return Err[String](err)
	}

	defer reader.Close()

	buffer := new(bytes.Buffer)
	if _, err := io.Copy(buffer, reader); err != nil {
		return Err[String](err)
	}

	return Ok(String(buffer.Bytes()))
}

// Flate compresses the wrapped String using the flate (zlib) compression algorithm
// and returns the compressed data as a String.
// It accepts an optional compression level. If no level is provided, it defaults to 7.
func (c compress) Flate(level ...int) String {
	// gzdeflate() php
	buffer := new(bytes.Buffer)

	l := 7
	if len(level) != 0 {
		l = level[0]
	}

	writer, _ := flate.NewWriter(buffer, l)

	_, _ = io.WriteString(writer, c.str.Std())
	_ = writer.Flush()
	_ = writer.Close()

	return String(buffer.Bytes())
}

// Flate decompresses the wrapped String using the flate (zlib) compression algorithm
// and returns the decompressed data as a Result[String].
func (d decompress) Flate() Result[String] {
	// gzinflate() php
	reader := flate.NewReader(d.str.Reader())
	defer reader.Close()

	buffer := new(bytes.Buffer)
	if _, err := io.Copy(buffer, reader); err != nil {
		return Err[String](err)
	}

	return Ok(String(buffer.Bytes()))
}
