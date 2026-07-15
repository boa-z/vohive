package upstreamproxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/boa-z/vowifi-go/runtimehost"
)

func TestSOCKS5UDPDatagramRoundTrip(t *testing.T) {
	frame, err := buildSOCKS5UDPDatagram("87.194.9.8:4500", []byte("payload"))
	if err != nil {
		t.Fatalf("build datagram: %v", err)
	}
	target, payload, err := parseSOCKS5UDPDatagram(frame)
	if err != nil {
		t.Fatalf("parse datagram: %v", err)
	}
	if target != "87.194.9.8:4500" {
		t.Fatalf("target = %q", target)
	}
	if !bytes.Equal(payload, []byte("payload")) {
		t.Fatalf("payload = %q", payload)
	}
}

func TestSOCKS5EndpointFromURL(t *testing.T) {
	endpoint, err := socks5EndpointFromProxy(&runtimehost.ProxyConfig{
		Enabled: true,
		Addr:    "socks5://alice:secret@127.0.0.1:1080",
	})
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	if endpoint.Address != "127.0.0.1:1080" || endpoint.Username != "alice" || endpoint.Password != "secret" {
		t.Fatalf("unexpected endpoint: %#v", endpoint)
	}
}

func TestSOCKS5SWUSharedIKEAndESPAssociation(t *testing.T) {
	server := newFakeSOCKS5UDPServer(t)
	defer server.Close()

	shared := newSOCKS5SWUTransport(socks5Endpoint{Address: server.Address()}, time.Second)
	if err := shared.configureRemote("87.194.9.8:4500", time.Second); err != nil {
		t.Fatalf("configure remote: %v", err)
	}
	defer shared.Close()

	ike := &socks5IKETransport{shared: shared, useNonESPMarker: true}
	ikeRequest := []byte("ike-request")
	ikeResponse, err := ike.ExchangeIKE(context.Background(), ikeRequest)
	if err != nil {
		t.Fatalf("IKE exchange: %v", err)
	}
	if !bytes.Equal(ikeResponse, ikeRequest) {
		t.Fatalf("IKE response = %x", ikeResponse)
	}

	esp := &socks5ESPTransport{shared: shared}
	espPacket := []byte{0x01, 0x02, 0x03, 0x04, 0xaa, 0xbb, 0xcc, 0xdd}
	if err := esp.SendESPPacket(context.Background(), espPacket); err != nil {
		t.Fatalf("send ESP: %v", err)
	}
	espResponse, err := esp.ReadESPPacket(context.Background())
	if err != nil {
		t.Fatalf("read ESP: %v", err)
	}
	if !bytes.Equal(espResponse, espPacket) {
		t.Fatalf("ESP response = %x", espResponse)
	}
}

type fakeSOCKS5UDPServer struct {
	t      *testing.T
	tcp    net.Listener
	udp    *net.UDPConn
	closed chan struct{}
}

func newFakeSOCKS5UDPServer(t *testing.T) *fakeSOCKS5UDPServer {
	t.Helper()
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen TCP: %v", err)
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_ = tcp.Close()
		t.Fatalf("listen UDP: %v", err)
	}
	server := &fakeSOCKS5UDPServer{t: t, tcp: tcp, udp: udp, closed: make(chan struct{})}
	go server.serveTCP()
	go server.serveUDP()
	return server
}

func (s *fakeSOCKS5UDPServer) Address() string {
	return s.tcp.Addr().String()
}

func (s *fakeSOCKS5UDPServer) Close() {
	select {
	case <-s.closed:
		return
	default:
		close(s.closed)
	}
	_ = s.tcp.Close()
	_ = s.udp.Close()
}

func (s *fakeSOCKS5UDPServer) serveTCP() {
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			return
		}
		go s.handleTCP(conn)
	}
}

func (s *fakeSOCKS5UDPServer) handleTCP(conn net.Conn) {
	defer conn.Close()
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{socks5Version, socks5AuthNone}); err != nil {
		return
	}

	var requestHeader [4]byte
	if _, err := io.ReadFull(conn, requestHeader[:]); err != nil {
		return
	}
	if _, err := readSOCKS5Host(conn, requestHeader[3]); err != nil {
		return
	}
	var port [2]byte
	if _, err := io.ReadFull(conn, port[:]); err != nil {
		return
	}

	relay := s.udp.LocalAddr().(*net.UDPAddr)
	response := []byte{socks5Version, socks5ReplySuccess, 0, socks5AtypIPv4, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(response[len(response)-2:], uint16(relay.Port))
	if _, err := conn.Write(response); err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, conn)
}

func (s *fakeSOCKS5UDPServer) serveUDP() {
	buf := make([]byte, socks5UDPMaxDatagramSize)
	for {
		n, client, err := s.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		target, payload, err := parseSOCKS5UDPDatagram(buf[:n])
		if err != nil {
			continue
		}
		response, err := buildSOCKS5UDPDatagram(target, payload)
		if err != nil {
			continue
		}
		_, _ = s.udp.WriteToUDP(response, client)
	}
}
