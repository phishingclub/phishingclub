package remotebrowser

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"time"
)

// The report PDF renderer runs a headless browser over operator authored report
// templates that may embed arbitrary external resources (images, scripts, styles).
// Those resources are fetched by the browser, so without a guard an injected or
// operator supplied URL pointing at an internal address turns the render into a
// server side request forgery against loopback, link local metadata, or private
// networks.
//
// guardedProxy is a small forward proxy the report browser is pointed at. It
// permits arbitrary public destinations but refuses any request whose target
// resolves to an internal address. It resolves the host and connects to that
// exact resolved address in one step, so a DNS record that flips to an internal
// address between resolution and connect (rebinding) cannot move the connection
// onto an internal host. It fails closed: any resolution or dial problem blocks
// the request rather than letting it through.
//
// The browser must also be started with proxy bypass for loopback removed, or it
// connects to loopback and link local metadata directly, skipping this guard.
type guardedProxy struct {
	ln        net.Listener
	srv       *http.Server
	transport *http.Transport
	blocked   atomic.Int64 // requests refused by policy (observability and tests)
}

// blockedCIDRs are ranges that must never be reached from the report browser.
// This is an explicit deny list layered on top of the IsGlobalUnicast allowlist
// in isInternalIP, covering reserved and special use ranges that are still
// classified as global unicast (RFC1918 and the reserved v4/v6 blocks below).
var blockedCIDRs = parseCIDRs(
	// IPv4
	"0.0.0.0/8",          // this host on this network
	"10.0.0.0/8",         // private
	"100.64.0.0/10",      // carrier grade NAT (some metadata services)
	"127.0.0.0/8",        // loopback
	"169.254.0.0/16",     // link local incl 169.254.169.254 metadata
	"172.16.0.0/12",      // private
	"192.0.0.0/24",       // IETF protocol assignments incl 192.0.0.170 NAT64 discovery
	"192.168.0.0/16",     // private
	"198.18.0.0/15",      // benchmarking
	"240.0.0.0/4",        // reserved
	"255.255.255.255/32", // limited broadcast
	"192.88.99.0/24", // 6to4 relay anycast
	// IPv6
	"::1/128",      // loopback
	"::/128",       // unspecified
	"64:ff9b::/96", // NAT64
	"2001::/32",    // Teredo (a v4 in v6 transition range, unused for content)
	"fc00::/7",     // unique local
	"fe80::/10",    // link local
	"fec0::/10",    // deprecated site local
)

func parseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

// isInternalIP reports whether an address must never be reached from the report
// browser. It is deny by default: only global unicast public addresses are
// allowed. Unknown or unparsable addresses are treated as internal so the caller
// fails closed. IPv6 forms that embed an IPv4 address (mapped, 6to4, NAT64, and
// the deprecated v4 compatible form) are unwrapped so an internal v4 target
// cannot be smuggled inside a v6 address.
func isInternalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if len(ip) == net.IPv6len {
		if embedded := embeddedV4(ip); embedded != nil && isInternalIP(embedded) {
			return true
		}
	}
	// deny anything that is not a global unicast address: loopback, link local,
	// unspecified, multicast, and broadcast all fall here.
	if !ip.IsGlobalUnicast() {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// embeddedV4 returns the IPv4 address wrapped inside a v6 6to4, NAT64, or v4
// compatible address, or nil if none is embedded. IPv4 mapped addresses are
// already handled by To4 in the caller.
func embeddedV4(ip net.IP) net.IP {
	if len(ip) != net.IPv6len {
		return nil
	}
	// 6to4 2002:AABB:CCDD::/16
	if ip[0] == 0x20 && ip[1] == 0x02 {
		return net.IPv4(ip[2], ip[3], ip[4], ip[5])
	}
	// NAT64 64:ff9b::/96
	if ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
		return net.IPv4(ip[12], ip[13], ip[14], ip[15])
	}
	// deprecated IPv4 compatible ::a.b.c.d (first 12 bytes zero, not the
	// unspecified or loopback addresses)
	for i := 0; i < 12; i++ {
		if ip[i] != 0 {
			return nil
		}
	}
	if ip[12] == 0 && ip[13] == 0 && ip[14] == 0 && (ip[15] == 0 || ip[15] == 1) {
		return nil
	}
	return net.IPv4(ip[12], ip[13], ip[14], ip[15])
}

// dialGuarded resolves the host in addr, selects a public address, and connects
// to that exact address. If no resolved address is public it returns an error and
// no connection is made.
func dialGuarded(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// malformed target, not a policy decision: fail closed but do not label it a block.
		return nil, &net.OpError{Op: "dial", Err: errUnresolvable}
	}
	var candidates []net.IP
	if literal := net.ParseIP(host); literal != nil {
		candidates = []net.IP{literal}
	} else {
		resolver := &net.Resolver{}
		ips, resErr := resolver.LookupIP(ctx, "ip", host)
		if resErr != nil {
			// could not resolve, not a policy decision: fail closed, do not count as a block.
			return nil, &net.OpError{Op: "dial", Err: errUnresolvable}
		}
		candidates = ips
	}
	for _, ip := range candidates {
		if isInternalIP(ip) {
			continue
		}
		d := net.Dialer{
			Timeout: 8 * time.Second,
			// recheck the concrete address at connect time as a second layer.
			Control: func(_, address string, _ syscall.RawConn) error {
				h, _, _ := net.SplitHostPort(address)
				if isInternalIP(net.ParseIP(h)) {
					return errBlocked
				}
				return nil
			},
		}
		return d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
	}
	return nil, &net.OpError{Op: "dial", Err: errBlocked}
}

// errBlocked marks a destination refused by policy (its address is internal).
// errUnresolvable marks a target that could not be parsed or resolved: also fails
// closed, but is reported as an upstream error rather than a policy block so the
// blocked counter and the 403 response mean only genuine internal refusals.
var (
	errBlocked      = blockedError("destination blocked by report render policy")
	errUnresolvable = blockedError("destination could not be resolved")
)

type blockedError string

func (e blockedError) Error() string { return string(e) }

// startGuardedProxy starts the proxy on an ephemeral loopback port.
func startGuardedProxy() (*guardedProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &guardedProxy{
		ln: ln,
		transport: &http.Transport{
			DialContext:           dialGuarded,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          32,
			IdleConnTimeout:       20 * time.Second,
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}
	p.srv = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// addr is the host:port the browser must be pointed at.
func (p *guardedProxy) addr() string { return p.ln.Addr().String() }

func (p *guardedProxy) close() {
	_ = p.srv.Close()
	p.transport.CloseIdleConnections()
}

// blockedRequests is the number of requests refused by policy so far.
func (p *guardedProxy) blockedRequests() int64 { return p.blocked.Load() }

// hopByHopHeaders are removed before forwarding in either direction per RFC 7230.
var hopByHopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func removeHopByHop(h http.Header) {
	for _, k := range hopByHopHeaders {
		h.Del(k)
	}
}

func (p *guardedProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	if !r.URL.IsAbs() {
		http.Error(w, "proxy requires absolute URI", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	removeHopByHop(out.Header)
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		if errors.Is(err, errBlocked) {
			p.blocked.Add(1)
			http.Error(w, "blocked by policy", http.StatusForbidden)
		} else {
			http.Error(w, "upstream error", http.StatusBadGateway)
		}
		return
	}
	defer resp.Body.Close()
	removeHopByHop(resp.Header)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body) // stream, never buffer whole bodies
}

// tunnelIdleTimeout bounds how long a CONNECT tunnel may sit with no data before
// it is torn down, so a slow or idle peer cannot hold a connection and goroutine
// indefinitely.
const tunnelIdleTimeout = 30 * time.Second

// handleConnect gates and tunnels a CONNECT (used for https). The target host is
// resolved and pinned by dialGuarded; the tunnel then streams opaque bytes.
func (p *guardedProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	upstream, err := dialGuarded(r.Context(), "tcp", r.Host)
	if err != nil {
		if errors.Is(err, errBlocked) {
			p.blocked.Add(1)
			http.Error(w, "blocked by policy", http.StatusForbidden)
		} else {
			http.Error(w, "upstream error", http.StatusBadGateway)
		}
		return
	}
	hij, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	client, _, err := hij.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	tunnelPipe(client, upstream)
}

// tunnelPipe streams bytes both ways between a CONNECT client and its upstream.
// It tears the tunnel down only after the whole tunnel has been idle (no data in
// either direction) for tunnelIdleTimeout, so a legitimate one directional
// transfer such as a large download is never killed while it is still
// progressing, while an idle tunnel cannot hold a connection open forever.
func tunnelPipe(client, upstream net.Conn) {
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	stop := make(chan struct{})

	go func() {
		ticker := time.NewTicker(tunnelIdleTimeout / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, lastActivity.Load())) >= tunnelIdleTimeout {
					_ = client.Close()
					_ = upstream.Close()
					return
				}
			}
		}
	}()

	pipe := func(dst, src net.Conn) {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				lastActivity.Store(time.Now().UnixNano())
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}

	go func() {
		pipe(upstream, client)
		_ = client.Close()
		_ = upstream.Close()
	}()
	pipe(client, upstream)
	_ = client.Close()
	_ = upstream.Close()
	close(stop)
}
