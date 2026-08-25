package fastresolver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/miekg/dns"
)

type handlerFunc func(w dns.ResponseWriter, r *dns.Msg)

type lookupFunc func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)

func (lookup lookupFunc) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	return lookup(ctx, name, qtype)
}

type mockDNSServer struct {
	udp     *dns.Server
	tcp     *dns.Server
	address string
}

func newTestResponse(name string, qtype uint16) *dns.Msg {
	request := new(dns.Msg).SetQuestion(dns.Fqdn(name), qtype)
	return new(dns.Msg).SetReply(request)
}

func firstRR[T dns.RR](records []dns.RR) (T, bool) {
	for _, record := range records {
		if typed, ok := record.(T); ok {
			return typed, true
		}
	}
	var zero T
	return zero, false
}

func startMockDNSServer(handler handlerFunc) (*mockDNSServer, error) {
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	address := tcpListener.Addr().String()
	udpConn, err := net.ListenPacket("udp", address)
	if err != nil {
		_ = tcpListener.Close()
		return nil, err
	}

	server := &mockDNSServer{
		udp:     &dns.Server{PacketConn: udpConn, Handler: dns.HandlerFunc(handler)},
		tcp:     &dns.Server{Listener: tcpListener, Handler: dns.HandlerFunc(handler)},
		address: address,
	}
	go func() { _ = server.udp.ActivateAndServe() }()
	go func() { _ = server.tcp.ActivateAndServe() }()
	return server, nil
}

func (s *mockDNSServer) Shutdown() {
	_ = s.udp.Shutdown()
	_ = s.tcp.Shutdown()
}

// startMockUDPServer starts a local UDP DNS server on a random free port.
func startMockUDPServer(t handlerFunc) (*dns.Server, error) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	server := &dns.Server{
		PacketConn: pc,
		Handler:    dns.HandlerFunc(t),
	}

	go func() {
		_ = server.ActivateAndServe()
	}()

	return server, nil
}

// startMockDoHServer starts a local httptest server that handles DoH queries.
func startMockDoHServer(t handlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/dns-message" {
			http.Error(w, "invalid accept header", http.StatusUnsupportedMediaType)
			return
		}

		var query []byte
		var err error

		if r.Method == http.MethodGet {
			dnsParam := r.URL.Query().Get("dns")
			query, err = base64.RawURLEncoding.DecodeString(dnsParam)
			if err != nil {
				query, err = base64.URLEncoding.DecodeString(dnsParam)
			}
		} else if r.Method == http.MethodPost {
			query, err = io.ReadAll(r.Body)
		}

		if err != nil || len(query) == 0 {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}

		var msg dns.Msg
		if err := msg.Unpack(query); err != nil {
			http.Error(w, "invalid dns msg", http.StatusBadRequest)
			return
		}

		recorder := &dohResponseWriter{w: w}
		t(recorder, &msg)
	}))
}

type dohResponseWriter struct {
	w http.ResponseWriter
}

func (d *dohResponseWriter) LocalAddr() net.Addr  { return nil }
func (d *dohResponseWriter) RemoteAddr() net.Addr { return nil }
func (d *dohResponseWriter) WriteMsg(m *dns.Msg) error {
	buf, err := m.Pack()
	if err != nil {
		return err
	}
	d.w.Header().Set("Content-Type", "application/dns-message")
	d.w.WriteHeader(http.StatusOK)
	_, err = d.w.Write(buf)
	return err
}
func (d *dohResponseWriter) Write(b []byte) (int, error) { return d.w.Write(b) }
func (d *dohResponseWriter) Close() error                { return nil }
func (d *dohResponseWriter) TsigStatus() error           { return nil }
func (d *dohResponseWriter) TsigTimersOnly(b bool)       {}
func (d *dohResponseWriter) Hijack()                     {}

// startMockJSONAPIServer starts a local httptest server that handles JSON API queries.
func startMockJSONAPIServer(handler func(name string, qtype uint16) JSONAPIResponse) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		qtypeStr := r.URL.Query().Get("type")

		qtype := dns.StringToType[strings.ToUpper(qtypeStr)]

		resp := handler(name, qtype)
		buf, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf)
	}))
}
