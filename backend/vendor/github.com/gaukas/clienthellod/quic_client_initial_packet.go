package clienthellod

import (
	"crypto/sha1" // skipcq: GSC-G505
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrNoQUICClientHello = errors.New("no QUIC ClientHello found in the packet")

type ClientInitialPacket struct {
	raw []byte

	QHdr *QUICHeader              `json:"quic_header,omitempty"`               // QUIC header, set by the caller
	QCH  *QUICClientHello         `json:"quic_client_hello,omitempty"`         // TLS ClientHello, set by the caller
	QTP  *QUICTransportParameters `json:"quic_transport_parameters,omitempty"` // QUIC Transport Parameters, set by the caller

	HexID     string `json:"cip_fp_id,omitempty"`  // normalized
	NumericID uint64 `json:"cip_fp_nid,omitempty"` // original

	UserAgent string `json:"user_agent,omitempty"` // User-Agent header, set by the caller
}

func ParseQUICCIP(p []byte) (*ClientInitialPacket, error) {
	qHdr, err := DecodeQUICHeaderAndFrames(p)
	if err != nil {
		return nil, err
	}
	qHdr.HID()

	cryptoFrame, err := ReassembleCRYPTOFrames(qHdr.Frames())
	if err != nil {
		return nil, err
	}

	if len(cryptoFrame) == 0 {
		return nil, fmt.Errorf("%w: no CRYPTO frames found in the packet", ErrNoQUICClientHello)
	}

	ch, err := ParseQUICClientHello(cryptoFrame)
	if err != nil {
		return nil, fmt.Errorf("%w, ParseQUICClientHello(): %v", ErrNoQUICClientHello, err)
	}
	ch.FingerprintID(true)  // normalized
	ch.FingerprintID(false) // original
	if ch.qtp != nil {
		ch.qtp.HID()
	} else {
		return nil, fmt.Errorf("%w: no QUIC Transport Parameters found in the packet", ErrNoQUICClientHello)
	}

	// Calculate fp
	h := sha1.New() // skipcq: GO-S1025, GSC-G401
	updateU64(h, qHdr.NID())
	updateU64(h, uint64(ch.FingerprintNID(true)))
	updateU64(h, ch.qtp.NumericID)
	NumericID := binary.BigEndian.Uint64(h.Sum(nil))
	hid := make([]byte, 8)
	binary.BigEndian.PutUint64(hid, NumericID)
	HexID := hex.EncodeToString(hid)

	return &ClientInitialPacket{
		raw:       p,
		QHdr:      qHdr,
		QCH:       ch,
		QTP:       ch.qtp,
		NumericID: NumericID,
		HexID:     HexID,
	}, nil
}
