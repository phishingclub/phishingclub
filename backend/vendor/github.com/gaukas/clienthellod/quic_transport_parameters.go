package clienthellod

import (
	"bytes"
	"crypto/sha1" // skipcq: GSC-G505
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/gaukas/clienthellod/internal/utils"
	"github.com/gaukas/godicttls"
)

const (
	QTP_GREASE = 27

	UNSET_VLI_BITS = true // if false, unsetVLIBits() will be nop
)

// QUICTransportParameters is a struct to hold the parsed QUIC transport parameters
// as a combination.
type QUICTransportParameters struct {
	MaxIdleTimeout                 utils.Uint8Arr `json:"max_idle_timeout,omitempty"`
	MaxUDPPayloadSize              utils.Uint8Arr `json:"max_udp_payload_size,omitempty"`
	InitialMaxData                 utils.Uint8Arr `json:"initial_max_data,omitempty"`
	InitialMaxStreamDataBidiLocal  utils.Uint8Arr `json:"initial_max_stream_data_bidi_local,omitempty"`
	InitialMaxStreamDataBidiRemote utils.Uint8Arr `json:"initial_max_stream_data_bidi_remote,omitempty"`
	InitialMaxStreamDataUni        utils.Uint8Arr `json:"initial_max_stream_data_uni,omitempty"`
	InitialMaxStreamsBidi          utils.Uint8Arr `json:"initial_max_streams_bidi,omitempty"`
	InitialMaxStreamsUni           utils.Uint8Arr `json:"initial_max_streams_uni,omitempty"`
	AckDelayExponent               utils.Uint8Arr `json:"ack_delay_exponent,omitempty"`
	MaxAckDelay                    utils.Uint8Arr `json:"max_ack_delay,omitempty"`

	ActiveConnectionIDLimit utils.Uint8Arr `json:"active_connection_id_limit,omitempty"`
	QTPIDs                  []uint64       `json:"qtpid,omitempty"` // sorted

	HexID     string `json:"tpfpid,omitempty"`
	NumericID uint64 `json:"tpfnid,omitempty"`

	parseError error
}

// ParseQUICTransportParameters parses the transport parameters from the extension data of
// TLS Extension "QUIC Transport Parameters" (57)
//
// If any error occurs, the returned struct will have parseError set to the error.
func ParseQUICTransportParameters(extData []byte) *QUICTransportParameters {
	qtp := &QUICTransportParameters{
		parseError: errors.New("unknown error"),
	}

	r := bytes.NewReader(extData)
	var paramType uint64
	var paramValLen uint64
	var paramData []byte
	var n int
	for r.Len() > 0 {
		paramType, _, qtp.parseError = ReadNextVLI(r)
		if qtp.parseError != nil {
			qtp.parseError = fmt.Errorf("failed to read transport parameter type: %w", qtp.parseError)
			return qtp
		}
		paramValLen, _, qtp.parseError = ReadNextVLI(r)
		if qtp.parseError != nil {
			qtp.parseError = fmt.Errorf("failed to read transport parameter value length: %w", qtp.parseError)
			return qtp
		}

		if IsGREASETransportParameter(paramType) {
			qtp.QTPIDs = append(qtp.QTPIDs, QTP_GREASE) // replace with placeholder
		} else {
			qtp.QTPIDs = append(qtp.QTPIDs, paramType)
		}

		if paramValLen == 0 {
			continue // skip empty transport parameter, no need to try to read
		}

		paramData = make([]byte, paramValLen)
		n, qtp.parseError = r.Read(paramData)
		if qtp.parseError != nil {
			qtp.parseError = fmt.Errorf("failed to read transport parameter value: %w", qtp.parseError)
			return qtp
		}
		if uint64(n) != paramValLen {
			qtp.parseError = errors.New("corrupted transport parameter")
			return qtp
		}

		switch paramType {
		case godicttls.QUICTransportParameter_max_idle_timeout:
			// qtp.MaxIdleTimeoutLength = uint32(paramValLen)
			qtp.MaxIdleTimeout = paramData
			unsetVLIBits(qtp.MaxIdleTimeout) // toggle the UNSET_VLI_BITS flag to control behavior
		case godicttls.QUICTransportParameter_max_udp_payload_size:
			// qtp.MaxUDPPayloadSizeLength = uint32(paramValLen)
			qtp.MaxUDPPayloadSize = paramData
			unsetVLIBits(qtp.MaxUDPPayloadSize)
		case godicttls.QUICTransportParameter_initial_max_data:
			// qtp.InitialMaxDataLength = uint32(paramValLen)
			qtp.InitialMaxData = paramData
			unsetVLIBits(qtp.InitialMaxData)
		case godicttls.QUICTransportParameter_initial_max_stream_data_bidi_local:
			// qtp.InitialMaxStreamDataBidiLocalLength = uint32(paramValLen)
			qtp.InitialMaxStreamDataBidiLocal = paramData
			unsetVLIBits(qtp.InitialMaxStreamDataBidiLocal)
		case godicttls.QUICTransportParameter_initial_max_stream_data_bidi_remote:
			// qtp.InitialMaxStreamDataBidiRemoteLength = uint32(paramValLen)
			qtp.InitialMaxStreamDataBidiRemote = paramData
			unsetVLIBits(qtp.InitialMaxStreamDataBidiRemote)
		case godicttls.QUICTransportParameter_initial_max_stream_data_uni:
			// qtp.InitialMaxStreamDataUniLength = uint32(paramValLen)
			qtp.InitialMaxStreamDataUni = paramData
			unsetVLIBits(qtp.InitialMaxStreamDataUni)
		case godicttls.QUICTransportParameter_initial_max_streams_bidi:
			// qtp.InitialMaxStreamsBidiLength = uint32(paramValLen)
			qtp.InitialMaxStreamsBidi = paramData
			unsetVLIBits(qtp.InitialMaxStreamsBidi)
		case godicttls.QUICTransportParameter_initial_max_streams_uni:
			// qtp.InitialMaxStreamsUniLength = uint32(paramValLen)
			qtp.InitialMaxStreamsUni = paramData
			unsetVLIBits(qtp.InitialMaxStreamsUni)
		case godicttls.QUICTransportParameter_ack_delay_exponent:
			// qtp.AckDelayExponentLength = uint32(paramValLen)
			qtp.AckDelayExponent = paramData
			unsetVLIBits(qtp.AckDelayExponent)
		case godicttls.QUICTransportParameter_max_ack_delay:
			// qtp.MaxAckDelayLength = uint32(paramValLen)
			qtp.MaxAckDelay = paramData
			unsetVLIBits(qtp.MaxAckDelay)
		case godicttls.QUICTransportParameter_active_connection_id_limit:
			// qtp.ActiveConnectionIDLimitLength = uint32(paramValLen)
			qtp.ActiveConnectionIDLimit = paramData
			unsetVLIBits(qtp.ActiveConnectionIDLimit)
		}

		// if IsGREASETransportParameter(paramType) {
		// 	qtp.QTPIDs = append(qtp.QTPIDs, QTP_GREASE) // replace with placeholder
		// } else {
		// 	qtp.QTPIDs = append(qtp.QTPIDs, paramType)
		// }
	}

	// sort QTPIDs
	sort.Slice(qtp.QTPIDs, func(i, j int) bool {
		return qtp.QTPIDs[i] < qtp.QTPIDs[j]
	})

	qtp.parseError = nil
	return qtp
}

// ParseError returns the error that occurred during parsing, if any.
func (qtp *QUICTransportParameters) ParseError() error {
	return qtp.parseError
}

// NID returns the numeric ID of this transport parameters combination.
func (qtp *QUICTransportParameters) NID() uint64 {
	if qtp.NumericID != 0 {
		return qtp.NumericID
	}

	h := sha1.New() // skipcq: GO-S1025, GSC-G401
	updateArr(h, qtp.MaxIdleTimeout)
	updateArr(h, qtp.MaxUDPPayloadSize)
	updateArr(h, qtp.InitialMaxData)
	updateArr(h, qtp.InitialMaxStreamDataBidiLocal)
	updateArr(h, qtp.InitialMaxStreamDataBidiRemote)
	updateArr(h, qtp.InitialMaxStreamDataUni)
	updateArr(h, qtp.InitialMaxStreamsBidi)
	updateArr(h, qtp.InitialMaxStreamsUni)
	updateArr(h, qtp.AckDelayExponent)
	updateArr(h, qtp.MaxAckDelay)
	updateArr(h, qtp.ActiveConnectionIDLimit)

	updateU32(h, uint32(len(qtp.QTPIDs)))
	for _, id := range qtp.QTPIDs {
		updateU64(h, id)
	}

	qtp.NumericID = binary.BigEndian.Uint64(h.Sum(nil))
	return qtp.NumericID
}

// HID returns the hex ID of this transport parameters combination.
func (qtp *QUICTransportParameters) HID() string {
	nid := qtp.NID()
	hid := make([]byte, 8)
	binary.BigEndian.PutUint64(hid, nid)

	qtp.HexID = hex.EncodeToString(hid)
	return qtp.HexID
}
