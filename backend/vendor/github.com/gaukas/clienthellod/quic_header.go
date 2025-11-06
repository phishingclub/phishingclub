package clienthellod

import (
	"bytes"
	"crypto/sha1" // skipcq: GSC-G505
	"encoding/binary"
	"encoding/hex"
	"errors"

	"github.com/gaukas/clienthellod/internal/utils"
	"golang.org/x/crypto/cryptobyte"
)

const (
	TOKEN_ABSENT  uint32 = 0x00000000
	TOKEN_PRESENT uint32 = 0x00000001
)

var (
	ErrNotQUICLongHeaderFormat = errors.New("packet is not in QUIC Long Header Format")
	ErrNotQUICInitialPacket    = errors.New("packet is not a QUIC Initial Packet")
)

// QUICHeader includes header fields of a QUIC packet and the following
// frames. It is used to calculate the fingerprint of a QUIC Header.
type QUICHeader struct {
	Version                   utils.Uint8Arr `json:"version,omitempty"` // 4-byte version
	DCIDLength                uint32         `json:"dcid_len,omitempty"`
	SCIDLength                uint32         `json:"scid_len,omitempty"`
	PacketNumber              utils.Uint8Arr `json:"pn,omitempty"` // VLI
	initialPacketNumberLength uint32
	initialPacketNumber       uint64

	// These two fields are not strictly part of QUIC header, but we need them before parsing QUIC ClientHello
	FrameIDs utils.Uint8Arr `json:"frame_id,omitempty"` // sorted
	frames   []Frame

	Token bool `json:"token,omitempty"`

	HexID     string `json:"hdrid,omitempty"`
	NumericID uint64 `json:"hdrnid,omitempty"`
}

// DecodeQUICHeaderAndFrames decodes a QUIC initial packet and returns a QUICHeader.
func DecodeQUICHeaderAndFrames(p []byte) (*QUICHeader, error) {
	if len(p) < 7 { // at least 7 bytes before TokenLength
		return nil, errors.New("packet too short")
	}

	// make a copy of the packet, so we can use it for crypto later
	recdata := make([]byte, len(p))
	copy(recdata, p)

	var qHdr *QUICHeader = &QUICHeader{}

	packetHeaderByteProtected := p[0]

	// check if it's in QUIC long header format:
	// - MSB highest bit is 1 (long header format)
	// - MSB 2nd highest bit is 1 (always set for QUIC)
	if packetHeaderByteProtected&0xc0 != 0xc0 {
		return nil, ErrNotQUICLongHeaderFormat
	}

	// check if it's a QUIC Initial Packet: MSB lower 2 bits are 0
	if packetHeaderByteProtected&0x30 != 0 {
		return nil, ErrNotQUICInitialPacket
	}

	// LSB of the first byte is protected, we will resolve it later

	qHdr.Version = p[1:5]
	s := cryptobyte.String(p[5:])
	initialRandom := new(cryptobyte.String)
	if !s.ReadUint8LengthPrefixed(initialRandom) {
		return nil, errors.New("failed to read DCID (initial random)")
	}
	qHdr.DCIDLength = uint32(len(*initialRandom))

	var scidLenUint8 uint8
	if !s.ReadUint8(&scidLenUint8) ||
		!s.Skip(int(scidLenUint8)) {
		return nil, errors.New("failed to read SCID")
	}
	qHdr.SCIDLength = uint32(scidLenUint8)

	// token length is a VLI
	r := bytes.NewReader(s)
	tokenLen, _, err := ReadNextVLI(r)
	if err != nil {
		return nil, err
	}
	// read token bytes
	token := make([]byte, tokenLen)
	n, err := r.Read(token)
	if err != nil {
		return nil, err
	}
	if n != int(tokenLen) {
		return nil, errors.New("failed to read all token bytes, short read")
	}
	if tokenLen > 0 {
		qHdr.Token = true
	}

	// packet length is a VLI
	packetLen, _, err := ReadNextVLI(r)
	if err != nil {
		return nil, err
	}
	if packetLen < 20 {
		return nil, errors.New("packet length too short, ignore")
	}

	// read all remaining bytes as payload
	payload := make([]byte, packetLen)
	n, err = r.Read(payload)
	if err != nil {
		return nil, err
	}
	if n != int(packetLen) {
		return nil, errors.New("failed to read all payload bytes, short read")
	}

	// do key calculation
	clientKey, clientIV, clientHpKey, err := ClientInitialKeysCalc(*initialRandom)
	if err != nil {
		return nil, err
	}

	// compute header protection
	hp, err := ComputeHeaderProtection(clientHpKey, payload[4:20])
	if err != nil {
		return nil, err
	}

	// prepare recdata
	// truncate recdata to remove following (possibly) padding bytes
	recdata = recdata[:len(recdata)-r.Len()]
	// remove payload bytes
	recdata = recdata[:len(recdata)-len(payload)] // recdata: [...headers...] [packet number]

	// decipher packet header byte
	headerByte := packetHeaderByteProtected ^ (hp[0] & 0x0f) // only lower 4 bits are protected and thus need to be XORed
	recdata[0] = headerByte
	qHdr.initialPacketNumberLength = uint32(headerByte&0x03) + 1 // LSB lower 2 bits are packet number length (-1)
	packetNumberBytes := payload[:qHdr.initialPacketNumberLength]
	for i, b := range packetNumberBytes {
		unprotectedByte := b ^ hp[i+1]
		recdata = append(recdata, unprotectedByte)
		qHdr.initialPacketNumber = qHdr.initialPacketNumber<<8 + uint64(unprotectedByte)
		qHdr.PacketNumber = append(qHdr.PacketNumber, unprotectedByte)
	}

	ciphertext := payload[qHdr.initialPacketNumberLength : len(payload)-16] // payload: [packet number (i-byte)] [encrypted data] [auth tag (16-byte)]
	authTag := payload[len(payload)-16:]

	// decipher payload
	plaintext, err := DecryptAES128GCM(clientIV, qHdr.initialPacketNumber, clientKey, ciphertext, recdata, authTag)
	if err != nil {
		return nil, err
	}

	// parse frames
	qHdr.frames, err = ReadAllFrames(bytes.NewBuffer(plaintext))
	if err != nil {
		return nil, err
	}

	for _, f := range qHdr.frames {
		qHdr.FrameIDs = append(qHdr.FrameIDs, uint8(f.FrameType()&0xff))
	}

	// deduplicate frame IDs
	qHdr.FrameIDs = utils.DedupIntArr(qHdr.FrameIDs)

	return qHdr, nil
}

// Frames returns all recognized frames in the QUIC header.
func (qHdr *QUICHeader) Frames() []Frame {
	return qHdr.frames
}

// NID returns a numeric fingerprint ID for the QUIC header.
func (qHdr *QUICHeader) NID() uint64 {
	if qHdr.NumericID != 0 {
		return qHdr.NumericID
	}

	h := sha1.New() // skipcq: GO-S1025, GSC-G401
	updateArr(h, qHdr.Version)
	updateU32(h, qHdr.DCIDLength)
	updateU32(h, qHdr.SCIDLength)
	updateArr(h, qHdr.PacketNumber)
	updateArr(h, qHdr.FrameIDs)
	if qHdr.Token {
		updateU32(h, TOKEN_PRESENT)
	} else {
		updateU32(h, TOKEN_ABSENT)
	}

	qHdr.NumericID = binary.BigEndian.Uint64(h.Sum(nil)[0:8])
	return qHdr.NumericID
}

// HID returns a hex fingerprint ID for the QUIC header.
func (qHdr *QUICHeader) HID() string {
	nid := qHdr.NID()
	hid := make([]byte, 8)
	binary.BigEndian.PutUint64(hid, nid)

	qHdr.HexID = hex.EncodeToString(hid)
	return qHdr.HexID
}
