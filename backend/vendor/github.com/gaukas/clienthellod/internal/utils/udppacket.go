package utils

import (
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// ParseUDPPacket parses the IP packet
func ParseUDPPacket(buf []byte) (*layers.UDP, error) {
	var udp *layers.UDP = &layers.UDP{}
	err := udp.DecodeFromBytes(buf, gopacket.NilDecodeFeedback)
	if err != nil {
		return nil, err
	}
	return udp, nil
}
