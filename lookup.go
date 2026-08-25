package fastresolver

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

type ILookup interface {
	Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)
}

var _ ILookup = (*Resolver)(nil)

// NewResolver creates a base resolver with a default 3-second timeout.
func NewResolver(server string) (*Resolver, error) {
	return NewResolverWithTimeout(server, 3*time.Second)
}

// NewResolverWithTimeout creates a base resolver with a custom socket timeout.
func NewResolverWithTimeout(server string, timeout time.Duration) (*Resolver, error) {
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		if !strings.Contains(err.Error(), "missing port in address") {
			return nil, err
		}
		host = server
		port = "53"
	}
	return &Resolver{
		server: net.JoinHostPort(host, port),
		udp: &dns.Client{
			Net:     "udp",
			Timeout: timeout,
		},
		tcp: &dns.Client{
			Net:     "tcp",
			Timeout: timeout,
		},
	}, nil
}

type Resolver struct {
	server string
	udp    *dns.Client
	tcp    *dns.Client
}

// Lookup implements ILookup.
func (resolver *Resolver) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	request := new(dns.Msg).SetQuestion(dns.Fqdn(name), qtype)
	return resolver.exchange(ctx, request)
}

func (resolver *Resolver) exchange(ctx context.Context, request *dns.Msg) (*dns.Msg, error) {
	udpConn, err := resolver.udp.DialContext(ctx, resolver.server)
	if err != nil {
		return nil, err
	}
	defer udpConn.Close()

	response, _, err := resolver.udp.ExchangeWithConnContext(ctx, request, udpConn)
	if err != nil {
		return response, err
	}
	if !response.Truncated {
		return validateResponse(request, response, resolver.server)
	}

	truncatedUDP := response
	tcpConn, err := resolver.tcp.DialContext(ctx, resolver.server)
	if err != nil {
		return truncatedUDP, err
	}
	defer tcpConn.Close()

	response, _, err = resolver.tcp.ExchangeWithConnContext(ctx, request, tcpConn)
	if err != nil {
		return truncatedUDP, err
	}
	if response.Truncated {
		return response, TruncatedError{
			Qname:  request.Question[0].Name,
			Server: resolver.server,
		}
	}
	return validateResponse(request, response, resolver.server)
}

// validateResponse enforces protocol-level response invariants for base transports.
func validateResponse(request, response *dns.Msg, server string) (*dns.Msg, error) {
	if response == nil {
		return nil, ErrNoResponse
	}
	if len(response.Question) == 0 {
		return response, ErrNoQuestion
	}
	if len(request.Question) != 1 || len(response.Question) != 1 {
		return response, fmt.Errorf("%w: expected one question, got %d", ErrInvalidQuestion, len(response.Question))
	}
	requestQuestion := request.Question[0]
	responseQuestion := response.Question[0]
	if !strings.EqualFold(requestQuestion.Name, responseQuestion.Name) {
		return response, fmt.Errorf(
			"%w: name %q does not match %q",
			ErrInvalidQuestion,
			responseQuestion.Name,
			requestQuestion.Name,
		)
	}
	if requestQuestion.Qtype != responseQuestion.Qtype {
		return response, fmt.Errorf(
			"%w: type %s does not match %s",
			ErrInvalidQuestion,
			dns.Type(responseQuestion.Qtype),
			dns.Type(requestQuestion.Qtype),
		)
	}
	if requestQuestion.Qclass != responseQuestion.Qclass {
		return response, fmt.Errorf(
			"%w: class %d does not match %d",
			ErrInvalidQuestion,
			responseQuestion.Qclass,
			requestQuestion.Qclass,
		)
	}
	if response.Rcode == dns.RcodeRefused {
		return response, ServerRefusedError{
			Qname:  response.Question[0].Name,
			Server: server,
		}
	}
	return response, nil
}

// normalizeLookupResult rejects the invalid empty success returned by custom resolvers.
func normalizeLookupResult(response *dns.Msg, err error) (*dns.Msg, error) {
	if response == nil && err == nil {
		return nil, ErrNoResponse
	}
	return response, err
}
