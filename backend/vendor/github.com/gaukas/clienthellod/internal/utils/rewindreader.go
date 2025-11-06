package utils

import (
	"bytes"
	"errors"
	"io"
)

// Interface guards
var (
	_ io.Reader = (*rewindReader)(nil)
)

type rewindReader struct {
	io.Reader
	rr bytes.Reader
}

func RewindReader(r io.Reader, buf []byte) io.Reader {
	if len(buf) == 0 {
		return r
	}

	return &rewindReader{
		Reader: r,
		rr:     *bytes.NewReader(buf),
	}
}

// Read implements io.Reader
// Read is ...
func (c *rewindReader) Read(b []byte) (int, error) {
	if c.rr.Size() == 0 {
		return c.Reader.Read(b)
	}
	n, err := c.rr.Read(b)
	if errors.Is(err, io.EOF) || c.rr.Len() == 0 {
		c.rr.Reset([]byte{})
		n2, err := c.Reader.Read(b[n:]) // read the rest if possible
		return n + n2, err
	}
	return n, err
}
