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
	URL    string
	Client *http.Client
}

func NewDoH(url string, timeout time.Duration) *DoH {
	return &DoH{
		URL:    url,
		Client: &http.Client{Timeout: timeout},
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
	u, err := url.Parse(d.URL)
	if err != nil {
		return ret, err
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return DNSRR{}, err
	}
	req.Header.Set("User-Agent", "github.com/lixianzheng/fastresolver/v2")
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
