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

func (d *DoH) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	var ret = DNSRR{
		ServerAddr: d.URL,
		Network:    "doh",
	}
	q := new(dns.Msg)
	q.SetQuestion(dns.CanonicalName(name), qtype)
	q.SetEdns0(1500, true)
	buf, err := q.Pack()
	if err != nil {
		return ret, err
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
			return ret, err
		}
		if u.Scheme == "" {
			u.Scheme = "https"
		}
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return DNSRR{}, err
	}
	req.Header.Set("User-Agent", "github.com/lixiangzhong/fastresolver/v2")
	req.Header.Set("Accept", "application/dns-message")
	t := time.Now()
	resp, err := d.Client.Do(req)
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ret, fmt.Errorf("status code: %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ret, err
	}
	ret.Rtt = time.Since(t)
	var reply dns.Msg
	if err := reply.Unpack(b); err != nil {
		return ret, err
	}
	err = toDNSRR(&reply, &ret)
	if err != nil {
		return ret, err
	}
	return ret, nil
}
