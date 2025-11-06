package clienthellod

import "bytes"

type QUICClientHello struct {
	ClientHello
}

func ParseQUICClientHello(p []byte) (*QUICClientHello, error) {
	// patch TLS record header to make it a valid TLS record
	record := make([]byte, 5+len(p))
	record[0] = 0x16 // TLS handshake
	record[1] = 0x00 // Dummy TLS version MSB
	record[2] = 0x00 // Dummy TLS version LSB
	record[3] = byte(len(p) >> 8)
	record[4] = byte(len(p))
	copy(record[5:], p)

	// parse TLS record
	r := bytes.NewReader(record)
	ch, err := ReadClientHello(r)
	if err != nil {
		return nil, err
	}

	if err = ch.ParseClientHello(); err != nil {
		return nil, err
	}

	if ch.qtp == nil {
		return nil, ErrNotQUICInitialPacket
	}

	if ch.qtp.ParseError() != nil {
		return nil, ch.qtp.ParseError()
	}

	return &QUICClientHello{ClientHello: *ch}, nil
}

func (qch *QUICClientHello) Raw() []byte {
	return qch.ClientHello.Raw()[5:] // strip TLS record header which is added by ParseQUICClientHello
}
