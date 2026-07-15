package upstreamproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/boa-z/vowifi-go/engine/swu"
	"github.com/boa-z/vowifi-go/engine/swu/ikev2"
	"github.com/boa-z/vowifi-go/runtimehost"
)

const socks5UDPMaxDatagramSize = 64 * 1024

var errSOCKS5SWUTransportClosed = errors.New("socks5 swu transport closed")

type socks5Endpoint struct {
	Address  string
	Username string
	Password string
}

type socks5UDPAssociation struct {
	tcpConn net.Conn
	udpConn *net.UDPConn
	relay   *net.UDPAddr
	timeout time.Duration

	readMu    sync.Mutex
	writeMu   sync.Mutex
	closeOnce sync.Once
}

type socks5SWUTransport struct {
	endpoint socks5Endpoint
	timeout  time.Duration

	mu         sync.Mutex
	remoteAddr string
	assoc      *socks5UDPAssociation
	closed     bool
	terminal   error
	done       chan struct{}
	ikeCh      chan []byte
	espCh      chan []byte
	ikeMu      sync.Mutex
	unmarked   atomic.Int32
}

type socks5IKETransport struct {
	shared          *socks5SWUTransport
	useNonESPMarker bool
}

type socks5ESPTransport struct {
	shared *socks5SWUTransport
}

type closeOnEstablishErrorManager struct {
	base   swu.TunnelManager
	shared *socks5SWUTransport
}

var (
	_ ikev2.InitTransport             = (*socks5IKETransport)(nil)
	_ swu.ESPPacketReadWriteTransport = (*socks5ESPTransport)(nil)
	_ swu.ESPPacketTransportCloser    = (*socks5ESPTransport)(nil)
	_ swu.NATTKeepaliveSender         = (*socks5ESPTransport)(nil)
)

// NewVoWiFiTunnelManager builds a userspace SWu tunnel manager whose IKE and
// ESP/NAT-T datagrams are both carried through the configured SOCKS5 UDP
// association. It intentionally fails closed when the proxy is unavailable;
// otherwise a country-routed VoWiFi session could silently leak to direct UDP.
func NewVoWiFiTunnelManager(req runtimehost.StartRequest) (swu.TunnelManager, error) {
	if req.SIM == nil {
		return nil, errors.New("proxied SWU tunnel manager requires SIM AKA provider")
	}
	if strings.TrimSpace(req.Dataplane.Mode) == swu.DataplaneModeKernel {
		return nil, errors.New("SOCKS5 proxied SWU requires userspace dataplane; kernel XFRM would bypass the proxy")
	}

	endpoint, err := socks5EndpointFromProxy(req.Proxy)
	if err != nil {
		return nil, err
	}
	shared := newSOCKS5SWUTransport(endpoint, 8*time.Second)

	ikeCfg := swu.IKEPacketTunnelManagerConfig{
		SIM:                     req.SIM,
		Reauthentication:        req.EAPReauthentication,
		OnReauthenticationState: req.OnEAPReauthenticationState,
		IKETransportFactory: func(_ swu.TunnelConfig, cfg swu.IKETransportConfig) (ikev2.InitTransport, error) {
			if err := shared.configureRemote(cfg.RemoteAddr, cfg.Timeout); err != nil {
				return nil, err
			}
			return &socks5IKETransport{
				shared:          shared,
				useNonESPMarker: cfg.UseNonESPMarker,
			}, nil
		},
		ESPTransportFactory: func(_ swu.TunnelConfig, cfg swu.ESPTransportConfig) (swu.ESPPacketTransport, error) {
			if err := shared.configureRemote(cfg.RemoteAddr, cfg.Timeout); err != nil {
				return nil, err
			}
			return &socks5ESPTransport{shared: shared}, nil
		},
	}

	base := swu.NewTUNIKETunnelManager(
		ikeCfg,
		swu.TUNTunnelManagerConfig{
			TUN:                 swu.TUNDeviceConfig{Name: strings.TrimSpace(req.Dataplane.TUNName)},
			DisableRouting:      req.Dataplane.DisableTUNRouting,
			DefaultRoutes:       true,
			ProtectEPDGRoutes:   true,
			MTU:                 req.Dataplane.TUNMTU,
			Addresses:           append([]string(nil), req.Dataplane.TUNAddresses...),
			EPDGRouteExclusions: cloneEPDGRouteExclusions(req.Dataplane.TUNEPDGExclusions),
			Routes:              append([]swu.TUNRoute(nil), req.Dataplane.TUNRoutes...),
			Rules:               append([]swu.TUNRule(nil), req.Dataplane.TUNRules...),
		},
	)
	return &closeOnEstablishErrorManager{base: base, shared: shared}, nil
}

func (m *closeOnEstablishErrorManager) EstablishTunnel(ctx context.Context, cfg swu.TunnelConfig) (swu.TunnelSession, error) {
	if m == nil || m.base == nil {
		return nil, errors.New("proxied SWU tunnel manager is nil")
	}
	session, err := m.base.EstablishTunnel(ctx, cfg)
	if err != nil && m.shared != nil {
		_ = m.shared.Close()
	}
	return session, err
}

func cloneEPDGRouteExclusions(in []swu.EPDGRouteExclusion) []swu.EPDGRouteExclusion {
	out := make([]swu.EPDGRouteExclusion, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Tables = append([]string(nil), item.Tables...)
	}
	return out
}

func socks5EndpointFromProxy(proxy *runtimehost.ProxyConfig) (socks5Endpoint, error) {
	if proxy == nil || !proxy.Enabled {
		return socks5Endpoint{}, errors.New("SOCKS5 proxy is not enabled")
	}
	raw := strings.TrimSpace(proxy.Addr)
	if raw == "" {
		raw = strings.TrimSpace(proxy.Address)
	}
	if raw == "" {
		raw = strings.TrimSpace(proxy.URL)
	}
	if raw == "" {
		return socks5Endpoint{}, errors.New("SOCKS5 proxy address is empty")
	}

	endpoint := socks5Endpoint{
		Address:  raw,
		Username: proxy.Username,
		Password: proxy.Password,
	}
	if !strings.Contains(raw, "://") {
		if _, _, err := net.SplitHostPort(raw); err != nil {
			return socks5Endpoint{}, fmt.Errorf("invalid SOCKS5 proxy address %q: %w", raw, err)
		}
		return endpoint, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return socks5Endpoint{}, fmt.Errorf("parse SOCKS5 proxy URL: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "socks5", "socks5h":
	default:
		return socks5Endpoint{}, fmt.Errorf("unsupported proxy scheme %q; SOCKS5 is required", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return socks5Endpoint{}, errors.New("SOCKS5 proxy URL has no host")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return socks5Endpoint{}, fmt.Errorf("invalid SOCKS5 proxy host %q: %w", u.Host, err)
	}
	endpoint.Address = u.Host
	if u.User != nil {
		if strings.TrimSpace(endpoint.Username) == "" {
			endpoint.Username = u.User.Username()
		}
		if endpoint.Password == "" {
			endpoint.Password, _ = u.User.Password()
		}
	}
	return endpoint, nil
}

func newSOCKS5SWUTransport(endpoint socks5Endpoint, timeout time.Duration) *socks5SWUTransport {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &socks5SWUTransport{
		endpoint: endpoint,
		timeout:  timeout,
		done:     make(chan struct{}),
		ikeCh:    make(chan []byte, 8),
		espCh:    make(chan []byte, 128),
	}
}

func (t *socks5SWUTransport) configureRemote(remoteAddr string, timeout time.Duration) error {
	if t == nil {
		return errors.New("SOCKS5 SWU transport is nil")
	}
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return errors.New("SWU remote address is empty")
	}
	if _, _, err := net.SplitHostPort(remoteAddr); err != nil {
		return fmt.Errorf("invalid SWU remote address %q: %w", remoteAddr, err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errSOCKS5SWUTransportClosed
	}
	if t.remoteAddr != "" && !strings.EqualFold(t.remoteAddr, remoteAddr) {
		return fmt.Errorf("SOCKS5 SWU remote changed from %q to %q", t.remoteAddr, remoteAddr)
	}
	t.remoteAddr = remoteAddr
	if timeout > 0 {
		t.timeout = timeout
	}
	return nil
}

func (t *socks5SWUTransport) ensureAssociation(ctx context.Context) (*socks5UDPAssociation, error) {
	if t == nil {
		return nil, errors.New("SOCKS5 SWU transport is nil")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, t.closedErrorLocked()
	}
	if t.assoc != nil {
		return t.assoc, nil
	}
	if strings.TrimSpace(t.remoteAddr) == "" {
		return nil, errors.New("SOCKS5 SWU remote address is not configured")
	}
	assoc, err := openSOCKS5UDPAssociation(ctx, t.endpoint, t.timeout)
	if err != nil {
		return nil, err
	}
	t.assoc = assoc
	go t.readLoop(assoc)
	return assoc, nil
}

func (t *socks5SWUTransport) readLoop(assoc *socks5UDPAssociation) {
	for {
		payload, err := assoc.readDatagram(context.Background(), 0)
		if err != nil {
			t.fail(err)
			return
		}
		if len(payload) == 1 && payload[0] == 0xff {
			continue
		}

		isMarkedIKE := len(payload) >= 4 && payload[0] == 0 && payload[1] == 0 && payload[2] == 0 && payload[3] == 0
		if isMarkedIKE || t.unmarked.Load() > 0 {
			select {
			case t.ikeCh <- payload:
			case <-t.done:
				return
			}
			continue
		}
		select {
		case t.espCh <- payload:
		case <-t.done:
			return
		}
	}
}

func (t *socks5SWUTransport) exchangeIKE(ctx context.Context, request []byte, useMarker bool) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	t.ikeMu.Lock()
	defer t.ikeMu.Unlock()

	assoc, err := t.ensureAssociation(ctx)
	if err != nil {
		return nil, err
	}
	wire := append([]byte(nil), request...)
	if useMarker {
		wire = append([]byte{0, 0, 0, 0}, wire...)
	} else {
		t.unmarked.Add(1)
		defer t.unmarked.Add(-1)
	}
	if err := assoc.writeDatagram(ctx, t.remote(), wire, t.timeoutValue()); err != nil {
		_ = t.Close()
		return nil, err
	}

	timer := time.NewTimer(t.timeoutValue())
	defer timer.Stop()
	select {
	case response := <-t.ikeCh:
		if len(response) >= 4 && response[0] == 0 && response[1] == 0 && response[2] == 0 && response[3] == 0 {
			response = response[4:]
		}
		return append([]byte(nil), response...), nil
	case <-ctx.Done():
		_ = t.Close()
		return nil, ctx.Err()
	case <-timer.C:
		timeoutErr := fmt.Errorf("SOCKS5 SWU IKE response timeout after %s", t.timeoutValue())
		_ = t.Close()
		return nil, timeoutErr
	case <-t.done:
		return nil, t.closedError()
	}
}

func (t *socks5SWUTransport) sendESP(ctx context.Context, payload []byte) error {
	assoc, err := t.ensureAssociation(ctx)
	if err != nil {
		return err
	}
	return assoc.writeDatagram(ctx, t.remote(), payload, t.timeoutValue())
}

func (t *socks5SWUTransport) readESP(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := t.ensureAssociation(ctx); err != nil {
		return nil, err
	}
	select {
	case payload := <-t.espCh:
		return append([]byte(nil), payload...), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.done:
		return nil, t.closedError()
	}
}

func (t *socks5SWUTransport) localAddr() net.Addr {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.assoc == nil || t.assoc.udpConn == nil {
		return nil
	}
	return t.assoc.udpConn.LocalAddr()
}

func (t *socks5SWUTransport) remote() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.remoteAddr
}

func (t *socks5SWUTransport) timeoutValue() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timeout <= 0 {
		return 8 * time.Second
	}
	return t.timeout
}

func (t *socks5SWUTransport) fail(err error) {
	if err == nil {
		err = errSOCKS5SWUTransportClosed
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.terminal = err
	assoc := t.assoc
	close(t.done)
	t.mu.Unlock()
	if assoc != nil {
		_ = assoc.Close()
	}
}

func (t *socks5SWUTransport) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if t.closed {
		err := t.terminal
		t.mu.Unlock()
		if errors.Is(err, errSOCKS5SWUTransportClosed) {
			return nil
		}
		return err
	}
	t.closed = true
	t.terminal = errSOCKS5SWUTransportClosed
	assoc := t.assoc
	close(t.done)
	t.mu.Unlock()
	if assoc != nil {
		return assoc.Close()
	}
	return nil
}

func (t *socks5SWUTransport) closedError() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closedErrorLocked()
}

func (t *socks5SWUTransport) closedErrorLocked() error {
	if t.terminal != nil {
		return t.terminal
	}
	return errSOCKS5SWUTransportClosed
}

func (t *socks5IKETransport) ExchangeIKE(ctx context.Context, request []byte) ([]byte, error) {
	if t == nil || t.shared == nil {
		return nil, errors.New("SOCKS5 IKE transport is nil")
	}
	return t.shared.exchangeIKE(ctx, request, t.useNonESPMarker)
}

func (t *socks5ESPTransport) SendESPPacket(ctx context.Context, packet []byte) error {
	if t == nil || t.shared == nil {
		return errors.New("SOCKS5 ESP transport is nil")
	}
	if len(packet) < 8 {
		return fmt.Errorf("ESP packet too short: %d bytes", len(packet))
	}
	if packet[0] == 0 && packet[1] == 0 && packet[2] == 0 && packet[3] == 0 {
		return errors.New("non-ESP marker cannot be sent as ESP")
	}
	return t.shared.sendESP(ctx, packet)
}

func (t *socks5ESPTransport) SendNATTKeepalive(ctx context.Context) error {
	if t == nil || t.shared == nil {
		return errors.New("SOCKS5 ESP transport is nil")
	}
	return t.shared.sendESP(ctx, []byte{0xff})
}

func (t *socks5ESPTransport) ReadESPPacket(ctx context.Context) ([]byte, error) {
	if t == nil || t.shared == nil {
		return nil, errors.New("SOCKS5 ESP transport is nil")
	}
	for {
		packet, err := t.shared.readESP(ctx)
		if err != nil {
			return nil, err
		}
		if len(packet) == 1 && packet[0] == 0xff {
			continue
		}
		if len(packet) >= 4 && packet[0] == 0 && packet[1] == 0 && packet[2] == 0 && packet[3] == 0 {
			continue
		}
		if len(packet) < 8 {
			return nil, fmt.Errorf("ESP packet too short: %d bytes", len(packet))
		}
		return packet, nil
	}
}

func (t *socks5ESPTransport) Close(context.Context) error {
	if t == nil || t.shared == nil {
		return nil
	}
	return t.shared.Close()
}

func (t *socks5ESPTransport) LocalNetworkAddr() net.Addr {
	if t == nil || t.shared == nil {
		return nil
	}
	return t.shared.localAddr()
}

func openSOCKS5UDPAssociation(ctx context.Context, endpoint socks5Endpoint, timeout time.Duration) (*socks5UDPAssociation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	tcpConn, err := dialer.DialContext(ctx, "tcp", endpoint.Address)
	if err != nil {
		return nil, fmt.Errorf("connect SOCKS5 proxy: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = tcpConn.Close()
		}
	}()

	if err := tcpConn.SetDeadline(deadlineFor(ctx, timeout)); err != nil {
		return nil, err
	}
	if _, err := probeHandshake(tcpConn, endpoint.Username, endpoint.Password); err != nil {
		return nil, err
	}
	if _, err := tcpConn.Write(buildProbeUDPAssociateRequest(probeUDPAssociateClientIP(tcpConn))); err != nil {
		return nil, fmt.Errorf("send SOCKS5 UDP ASSOCIATE: %w", err)
	}
	relay, err := readSOCKS5RelayAddress(tcpConn)
	if err != nil {
		return nil, err
	}
	if relay.IP == nil || relay.IP.IsUnspecified() {
		tcpHost, _, splitErr := net.SplitHostPort(tcpConn.RemoteAddr().String())
		if splitErr != nil {
			return nil, fmt.Errorf("resolve SOCKS5 relay fallback: %w", splitErr)
		}
		resolved, resolveErr := net.ResolveUDPAddr("udp", net.JoinHostPort(tcpHost, strconv.Itoa(relay.Port)))
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve SOCKS5 relay: %w", resolveErr)
		}
		relay = resolved
	}

	network := "udp4"
	if relay.IP.To4() == nil {
		network = "udp6"
	}
	udpConn, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil, fmt.Errorf("open SOCKS5 UDP relay socket: %w", err)
	}
	if err := tcpConn.SetDeadline(time.Time{}); err != nil {
		_ = udpConn.Close()
		return nil, err
	}
	ok = true
	return &socks5UDPAssociation{
		tcpConn: tcpConn,
		udpConn: udpConn,
		relay:   relay,
		timeout: timeout,
	}, nil
}

func readSOCKS5RelayAddress(r io.Reader) (*net.UDPAddr, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read SOCKS5 UDP ASSOCIATE header: %w", err)
	}
	if header[0] != socks5Version {
		return nil, fmt.Errorf("invalid SOCKS5 UDP ASSOCIATE version 0x%02x", header[0])
	}
	if header[1] != socks5ReplySuccess {
		return nil, fmt.Errorf("SOCKS5 UDP ASSOCIATE rejected with status 0x%02x", header[1])
	}
	host, err := readSOCKS5Host(r, header[3])
	if err != nil {
		return nil, err
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, portBytes); err != nil {
		return nil, fmt.Errorf("read SOCKS5 relay port: %w", err)
	}
	port := int(binary.BigEndian.Uint16(portBytes))
	return net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
}

func readSOCKS5Host(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case socks5AtypIPv4:
		buf := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", fmt.Errorf("read SOCKS5 IPv4 address: %w", err)
		}
		return net.IP(buf).String(), nil
	case socks5AtypIPv6:
		buf := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", fmt.Errorf("read SOCKS5 IPv6 address: %w", err)
		}
		return net.IP(buf).String(), nil
	case socks5AtypDomain:
		var length [1]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return "", fmt.Errorf("read SOCKS5 domain length: %w", err)
		}
		buf := make([]byte, int(length[0]))
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", fmt.Errorf("read SOCKS5 domain: %w", err)
		}
		return string(buf), nil
	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type 0x%02x", atyp)
	}
}

func (a *socks5UDPAssociation) writeDatagram(ctx context.Context, target string, payload []byte, timeout time.Duration) error {
	if a == nil || a.udpConn == nil || a.relay == nil {
		return errors.New("SOCKS5 UDP association is not ready")
	}
	frame, err := buildSOCKS5UDPDatagram(target, payload)
	if err != nil {
		return err
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if err := a.udpConn.SetWriteDeadline(deadlineFor(ctx, firstPositive(timeout, a.timeout))); err != nil {
		return err
	}
	_, err = a.udpConn.WriteToUDP(frame, a.relay)
	if err != nil {
		return fmt.Errorf("write SOCKS5 UDP datagram: %w", err)
	}
	return nil
}

func (a *socks5UDPAssociation) readDatagram(ctx context.Context, timeout time.Duration) ([]byte, error) {
	if a == nil || a.udpConn == nil {
		return nil, errors.New("SOCKS5 UDP association is not ready")
	}
	a.readMu.Lock()
	defer a.readMu.Unlock()
	deadline := time.Time{}
	if timeout > 0 || (ctx != nil && hasDeadline(ctx)) {
		deadline = deadlineFor(ctx, timeout)
	}
	if err := a.udpConn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	buf := make([]byte, socks5UDPMaxDatagramSize)
	n, _, err := a.udpConn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	_, payload, err := parseSOCKS5UDPDatagram(buf[:n])
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (a *socks5UDPAssociation) Close() error {
	if a == nil {
		return nil
	}
	var closeErr error
	a.closeOnce.Do(func() {
		if a.udpConn != nil {
			closeErr = errors.Join(closeErr, a.udpConn.Close())
		}
		if a.tcpConn != nil {
			closeErr = errors.Join(closeErr, a.tcpConn.Close())
		}
	})
	return closeErr
}

func buildSOCKS5UDPDatagram(target string, payload []byte) ([]byte, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(target))
	if err != nil {
		return nil, fmt.Errorf("parse SOCKS5 UDP target %q: %w", target, err)
	}
	port64, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port64 == 0 {
		return nil, fmt.Errorf("invalid SOCKS5 UDP target port %q", portText)
	}
	addr, err := encodeSOCKS5Address(strings.Trim(host, "[]"))
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 0, 3+len(addr)+2+len(payload))
	frame = append(frame, 0, 0, 0)
	frame = append(frame, addr...)
	var port [2]byte
	binary.BigEndian.PutUint16(port[:], uint16(port64))
	frame = append(frame, port[:]...)
	frame = append(frame, payload...)
	return frame, nil
}

func parseSOCKS5UDPDatagram(frame []byte) (string, []byte, error) {
	if len(frame) < 4 {
		return "", nil, errors.New("SOCKS5 UDP datagram is too short")
	}
	if frame[0] != 0 || frame[1] != 0 {
		return "", nil, errors.New("invalid SOCKS5 UDP reserved field")
	}
	if frame[2] != 0 {
		return "", nil, fmt.Errorf("fragmented SOCKS5 UDP datagram is unsupported (FRAG=%d)", frame[2])
	}
	host, next, err := parseSOCKS5Address(frame, 3)
	if err != nil {
		return "", nil, err
	}
	if len(frame) < next+2 {
		return "", nil, errors.New("SOCKS5 UDP datagram has no port")
	}
	port := binary.BigEndian.Uint16(frame[next : next+2])
	return net.JoinHostPort(host, strconv.Itoa(int(port))), append([]byte(nil), frame[next+2:]...), nil
}

func encodeSOCKS5Address(host string) ([]byte, error) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return append([]byte{socks5AtypIPv4}, v4...), nil
		}
		v6 := ip.To16()
		if v6 == nil {
			return nil, fmt.Errorf("invalid IP address %q", host)
		}
		return append([]byte{socks5AtypIPv6}, v6...), nil
	}
	if host == "" || len(host) > 255 {
		return nil, fmt.Errorf("invalid SOCKS5 domain length %d", len(host))
	}
	out := make([]byte, 0, 2+len(host))
	out = append(out, socks5AtypDomain, byte(len(host)))
	out = append(out, host...)
	return out, nil
}

func parseSOCKS5Address(frame []byte, offset int) (string, int, error) {
	if offset >= len(frame) {
		return "", 0, errors.New("SOCKS5 address type is missing")
	}
	switch frame[offset] {
	case socks5AtypIPv4:
		end := offset + 1 + net.IPv4len
		if end > len(frame) {
			return "", 0, errors.New("truncated SOCKS5 IPv4 address")
		}
		return net.IP(frame[offset+1 : end]).String(), end, nil
	case socks5AtypIPv6:
		end := offset + 1 + net.IPv6len
		if end > len(frame) {
			return "", 0, errors.New("truncated SOCKS5 IPv6 address")
		}
		return net.IP(frame[offset+1 : end]).String(), end, nil
	case socks5AtypDomain:
		if offset+1 >= len(frame) {
			return "", 0, errors.New("SOCKS5 domain length is missing")
		}
		length := int(frame[offset+1])
		end := offset + 2 + length
		if end > len(frame) {
			return "", 0, errors.New("truncated SOCKS5 domain")
		}
		return string(frame[offset+2 : end]), end, nil
	default:
		return "", 0, fmt.Errorf("unsupported SOCKS5 address type 0x%02x", frame[offset])
	}
}

func deadlineFor(ctx context.Context, timeout time.Duration) time.Time {
	var deadline time.Time
	if ctx != nil {
		if ctxDeadline, ok := ctx.Deadline(); ok {
			deadline = ctxDeadline
		}
	}
	if timeout > 0 {
		timeoutDeadline := time.Now().Add(timeout)
		if deadline.IsZero() || timeoutDeadline.Before(deadline) {
			deadline = timeoutDeadline
		}
	}
	return deadline
}

func hasDeadline(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Deadline()
	return ok
}

func firstPositive(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
