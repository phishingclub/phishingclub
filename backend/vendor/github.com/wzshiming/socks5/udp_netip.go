//go:build go1.18
// +build go1.18

package socks5

import (
	"net"
	"net/netip"
)

// ReadFromUDPAddrPort implements the net.UDPConn ReadFromUDPAddrPort method.
func (c *UDPConn) ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, addr, errUnsupportedMethod
	}
	return udpConn.ReadFromUDPAddrPort(b)
}

// ReadMsgUDPAddrPort implements the net.UDPConn ReadMsgUDPAddrPort method.
func (c *UDPConn) ReadMsgUDPAddrPort(b, oob []byte) (n, oobn, flags int, addr netip.AddrPort, err error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, 0, 0, addr, errUnsupportedMethod
	}
	return udpConn.ReadMsgUDPAddrPort(b, oob)
}

// WriteToUDPAddrPort implements the net.UDPConn WriteToUDPAddrPort method.
func (c *UDPConn) WriteToUDPAddrPort(b []byte, addr netip.AddrPort) (int, error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, errUnsupportedMethod
	}
	return udpConn.WriteToUDPAddrPort(b, addr)
}

// WriteMsgUDPAddrPort implements the net.UDPConn WriteMsgUDPAddrPort method.
func (c *UDPConn) WriteMsgUDPAddrPort(b, oob []byte, addr netip.AddrPort) (n, oobn int, err error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, 0, errUnsupportedMethod
	}
	return udpConn.WriteMsgUDPAddrPort(b, oob, addr)
}
