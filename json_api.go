package fastresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/miekg/dns"
)

var _ ILookup = (*JSONAPI)(nil)

type JSONAPI struct {
	baseURL string
	http    *http.Client
}

// NewJSONAPI creates a JSON API resolver reusing the default shared http.Transport.
func NewJSONAPI(url string, timeout time.Duration) *JSONAPI {
	return &JSONAPI{
		baseURL: url,
		http: &http.Client{
			Timeout:   timeout,
			Transport: defaultTransport,
		},
	}
}

// NewJSONAPIWithClient creates a JSON API resolver with a custom http.Client.
func NewJSONAPIWithClient(url string, client *http.Client) *JSONAPI {
	return &JSONAPI{
		baseURL: url,
		http:    client,
	}
}

func (j *JSONAPI) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	ret := DNSRR{
		ServerAddr: j.baseURL,
		Network:    "dns json api",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.baseURL, nil)
	if err != nil {
		return ret, err
	}
	q := req.URL.Query()
	q.Set("name", name)
	q.Set("type", dns.TypeToString[qtype])
	req.URL.RawQuery = q.Encode()
	resp, err := j.http.Do(req)
	if err != nil {
		return ret, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ret, err
	}
	var r JSONAPIResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return ret, err
	}
	switch r.Status {
	case dns.RcodeSuccess:
	case dns.RcodeNameError:
		ret.NXDomain = true
		return ret, nil
	case dns.RcodeRefused:
		return ret, ServerRefusedError{Qname: name, Server: j.baseURL}
	}
	if r.Error != "" {
		return ret, fmt.Errorf("error: %s", r.Error)
	}
	var minTTL uint32 = 0

	for _, a := range r.Answer {
		if minTTL == 0 || a.TTL < minTTL {
			minTTL = a.TTL
		}
		switch a.Type {
		case dns.TypeA:
			ret.A = append(ret.A, a.Data)
		case dns.TypeAAAA:
			ret.AAAA = append(ret.AAAA, a.Data)
		case dns.TypeNS:
			ret.NS = append(ret.NS, a.Data)
		case dns.TypeCNAME:
			ret.CNAME = append(ret.CNAME, a.Data)
		case dns.TypeMX:
			ret.MX = append(ret.MX, MX{
				Value: a.Data,
			})
		case dns.TypeTXT:
			ret.TXT = append(ret.TXT, a.Data)
		case dns.TypeSRV:
			ret.SRV = append(ret.SRV, a.Data)
		case dns.TypeSOA:
		}
	}
	for _, v := range r.Authority {
		if minTTL == 0 || v.TTL < minTTL {
			minTTL = v.TTL
		}
		if strings.HasSuffix(dns.CanonicalName(name), v.Name) &&
			v.Type == dns.TypeSOA &&
			len(r.Answer) == 0 && qtype != dns.TypeSOA {
			ret.NXDomain = true
		}
	}

	if minTTL > 0 {
		ret.TTL = time.Duration(minTTL) * time.Second
	}

	return ret, nil
}

type JSONAPIResponse struct {
	Status    int
	TC        bool // Truncated
	RD        bool // Recursion Desired
	RA        bool // Recursion Available
	AD        bool // Authentic Data
	CD        bool // Checking Disabled
	Question  JSONAPIQuestions
	Answer    []JSONAPIAnswer
	Authority []JSONAPIAnswer
	Comment   string
	Error     string
}

var _ json.Unmarshaler = (*JSONAPIQuestions)(nil)

type JSONAPIQuestions []JSONAPIQuestion

func (q *JSONAPIQuestions) UnmarshalJSON(b []byte) error {
	var v []JSONAPIQuestion
	if err := json.Unmarshal(b, &v); err == nil {
		*q = v
		return nil
	}
	var single JSONAPIQuestion
	if err := json.Unmarshal(b, &single); err != nil {
		return err
	}
	*q = []JSONAPIQuestion{single}
	return nil
}

type JSONAPIQuestion struct {
	Name string `json:"name"`
	Type uint16 `json:"type"`
}

type JSONAPIAnswer struct {
	Name string `json:"name"`
	Type uint16 `json:"type"`
	TTL  uint32 `json:"ttl"`
	Data string `json:"data"`
}
