package clienthellod

import (
	"bytes"
	"errors"
	"io"
)

// ReadNextVLI unpacks the next variable-length integer from the given
// io.Reader. It returns the decoded value and the number of bytes read.
// For example:
//
//	0x0a -> 0xa, 1
//	0x80 0x10 0x00 0x00 -> 0x100000, 4
func ReadNextVLI(r io.Reader) (val uint64, n int, err error) {
	// read the first byte
	var encodedBytes []byte = make([]byte, 1)
	_, err = r.Read(encodedBytes)
	if err != nil {
		return 0, 0, err
	}

	// check MSBs of the first byte
	switch encodedBytes[0] & 0xc0 { // 0xc0 = 0b11000000, when the first 2 bits in a byte is set
	case 0x00:
		n = 1
	case 0x40:
		n = 2
	case 0x80:
		n = 4
	case 0xc0:
		n = 8
	default:
		return 0, 0, errors.New("invalid first byte")
	}

	// read the rest bytes
	if n > 1 {
		encodedBytes = append(encodedBytes, make([]byte, n-1)...)
		_, err = r.Read(encodedBytes[1:])
		if err != nil {
			return 0, 0, err
		}
	}

	// decode
	encodedBytes[0] &= 0x3f // 0x3f = 0b00111111, clear MSBs
	for i := 0; i < n; i++ {
		val <<= 8
		val |= uint64(encodedBytes[i])
	}

	return
}

func DecodeVLI(vli []byte) (val uint64, err error) {
	var n int
	val, n, err = ReadNextVLI(bytes.NewReader(vli))
	if err != nil {
		return 0, err
	}
	if n != len(vli) {
		return 0, errors.New("invalid VLI length")
	}
	return
}

func unsetVLIBits(vli []byte) {
	if UNSET_VLI_BITS {
		vli[0] &= 0x3f // 0x3f = 0b00111111, clear MSBs
	}
}

func IsGREASETransportParameter(paramType uint64) bool {
	return paramType >= 27 && (paramType-27)%31 == 0 // reserved values are 27, 58, 89, ...
}
