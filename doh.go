package fastresolver

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/miekg/dns"
)

var _ ILookup = (*DoH)(nil)

type DoH struct {
	URL       string
	parsedURL *url.URL
	Client    *http.Client
}

// NewDoH creates a DoH resolver reusing the default shared http.Transport.
func NewDoH(urlStr string, timeout time.Duration) *DoH {
	u, _ := url.Parse(urlStr)
	if u != nil && u.Scheme == "" {
		u.Scheme = "https"
	}
	return &DoH{
		URL:       urlStr,
		parsedURL: u,
		Client: &http.Client{
			Timeout:   timeout,
			Transport: defaultTransport,
		},
	}
}

// NewDoHWithClient creates a DoH resolver with a custom http.Client.
func NewDoHWithClient(urlStr string, client *http.Client) *DoH {
	u, _ := url.Parse(urlStr)
	if u != nil && u.Scheme == "" {
		u.Scheme = "https"
	}
	return &DoH{
		URL:       urlStr,
		parsedURL: u,
		Client:    client,
	}
}

func (d *DoH) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	q := new(dns.Msg)
	q.SetQuestion(dns.CanonicalName(name), qtype)
	q.SetEdns0(1500, true)
	requestID := q.Id
	if requestID == 0 {
		requestID = 1
	}
	q.Id = 0
	buf, err := q.Pack()
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"dns": []string{base64.RawURLEncoding.EncodeToString(buf)},
	}
	var u *url.URL
	if d.parsedURL != nil {
		// 复制一份 url.URL 避免并发场景下竞争修改同一个 u.RawQuery
		temp := *d.parsedURL
		u = &temp
	} else {
		var err error
		u, err = url.Parse(d.URL)
		if err != nil {
			return nil, err
		}
		if u.Scheme == "" {
			u.Scheme = "https"
		}
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "github.com/lixiangzhong/fastresolver/v3")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	reply := new(dns.Msg)
	if err := reply.Unpack(b); err != nil {
		return nil, err
	}
	if reply.Id != 0 {
		return reply, fmt.Errorf("%w: got %d, want 0", ErrInvalidResponseID, reply.Id)
	}
	reply.Id = requestID
	return validateResponse(q, reply, d.URL)
}
