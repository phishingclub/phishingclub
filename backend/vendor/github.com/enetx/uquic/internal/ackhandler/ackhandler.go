package ackhandler

import (
	"github.com/enetx/uquic/internal/protocol"
	"github.com/enetx/uquic/internal/utils"
	"github.com/enetx/uquic/logging"
)

// NewAckHandler creates a new SentPacketHandler and a new ReceivedPacketHandler.
// clientAddressValidated indicates whether the address was validated beforehand by an address validation token.
// clientAddressValidated has no effect for a client.
func NewAckHandler(
	initialPacketNumber protocol.PacketNumber,
	initialMaxDatagramSize protocol.ByteCount,
	rttStats *utils.RTTStats,
	clientAddressValidated bool,
	enableECN bool,
	pers protocol.Perspective,
	tracer *logging.ConnectionTracer,
	logger utils.Logger,
) (SentPacketHandler, ReceivedPacketHandler) {
	sph := newSentPacketHandler(initialPacketNumber, initialMaxDatagramSize, rttStats, clientAddressValidated, enableECN, pers, tracer, logger)
	return sph, newReceivedPacketHandler(sph, logger)
}
