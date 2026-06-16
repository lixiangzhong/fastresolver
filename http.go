package fastresolver

import (
	"net"
	"net/http"
	"time"
)

// defaultTransport is a shared, performance-optimized transport used by default
// for HTTP-based DNS lookups (DoH & JSON API) to maximize connection reuse.
var defaultTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   100, // Optimize for DNS resolving where requests go to few hosts
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}
