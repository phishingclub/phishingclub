package socks5

import (
	"bytes"
	"errors"
	"net"
	"time"
)

var (
	errBadHeader         = errors.New("bad header")
	errUnsupportedMethod = errors.New("unsupported method")
)

type UDPConn struct {
	bufRead       [maxUdpPacket]byte
	bufWrite      [maxUdpPacket]byte
	proxyAddress  net.Addr
	defaultTarget net.Addr
	prefix        []byte
	net.PacketConn
}

func NewUDPConn(raw net.PacketConn, proxyAddress net.Addr, defaultTarget net.Addr) (*UDPConn, error) {
	conn := &UDPConn{
		PacketConn:    raw,
		proxyAddress:  proxyAddress,
		defaultTarget: defaultTarget,
		prefix:        []byte{0, 0, 0},
	}
	return conn, nil
}

// ReadFrom implements the net.PacketConn ReadFrom method.
func (c *UDPConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, addr, err = c.PacketConn.ReadFrom(c.bufRead[:])
	if err != nil {
		return 0, nil, err
	}
	if n < len(c.prefix) || addr.String() != c.proxyAddress.String() {
		return 0, nil, errBadHeader
	}
	buf := bytes.NewBuffer(c.bufRead[len(c.prefix):n])
	a, err := readAddr(buf)
	if err != nil {
		return 0, nil, err
	}
	n = copy(p, buf.Bytes())
	return n, a, nil
}

// WriteTo implements the net.PacketConn WriteTo method.
func (c *UDPConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	buf := bytes.NewBuffer(c.bufWrite[:0])
	buf.Write(c.prefix)
	err = writeAddrWithStr(buf, addr.String())
	if err != nil {
		return 0, err
	}
	n, err = buf.Write(p)
	if err != nil {
		return 0, err
	}

	data := buf.Bytes()
	_, err = c.PacketConn.WriteTo(data, c.proxyAddress)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Read implements the net.Conn Read method.
func (c *UDPConn) Read(b []byte) (int, error) {
	n, addr, err := c.ReadFrom(b)
	if err != nil {
		return 0, err
	}
	if addr.String() != c.defaultTarget.String() {
		return c.Read(b)
	}
	return n, nil
}

// Write implements the net.Conn Write method.
func (c *UDPConn) Write(b []byte) (int, error) {
	return c.WriteTo(b, c.defaultTarget)
}

// RemoteAddr implements the net.Conn RemoteAddr method.
func (c *UDPConn) RemoteAddr() net.Addr {
	return c.defaultTarget
}

// SetReadBuffer implements the net.UDPConn SetReadBuffer method.
func (c *UDPConn) SetReadBuffer(bytes int) error {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return errUnsupportedMethod
	}
	return udpConn.SetReadBuffer(bytes)
}

// SetWriteBuffer implements the net.UDPConn SetWriteBuffer method.
func (c *UDPConn) SetWriteBuffer(bytes int) error {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return errUnsupportedMethod
	}
	return udpConn.SetWriteBuffer(bytes)
}

// SetDeadline implements the Conn SetDeadline method.
func (c *UDPConn) SetDeadline(t time.Time) error {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return errUnsupportedMethod
	}
	return udpConn.SetDeadline(t)
}

// SetReadDeadline implements the Conn SetReadDeadline method.
func (c *UDPConn) SetReadDeadline(t time.Time) error {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return errUnsupportedMethod
	}
	return udpConn.SetReadDeadline(t)
}

// SetWriteDeadline implements the Conn SetWriteDeadline method.
func (c *UDPConn) SetWriteDeadline(t time.Time) error {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return errUnsupportedMethod
	}
	return udpConn.SetWriteDeadline(t)
}

// ReadFromUDP implements the net.UDPConn ReadFromUDP method.
func (c *UDPConn) ReadFromUDP(b []byte) (n int, addr *net.UDPAddr, err error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, nil, errUnsupportedMethod
	}
	return udpConn.ReadFromUDP(b)
}

// ReadMsgUDP implements the net.UDPConn ReadMsgUDP method.
func (c *UDPConn) ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, 0, 0, nil, errUnsupportedMethod
	}
	return udpConn.ReadMsgUDP(b, oob)
}

// WriteToUDP implements the net.UDPConn WriteToUDP method.
func (c *UDPConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, errUnsupportedMethod
	}
	return udpConn.WriteToUDP(b, addr)
}

// WriteMsgUDP implements the net.UDPConn WriteMsgUDP method.
func (c *UDPConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (n, oobn int, err error) {
	udpConn, ok := c.PacketConn.(*net.UDPConn)
	if !ok {
		return 0, 0, errUnsupportedMethod
	}
	return udpConn.WriteMsgUDP(b, oob, addr)
}
