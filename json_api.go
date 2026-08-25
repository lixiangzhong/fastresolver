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
	return &JSONAPI{baseURL: url, http: client}
}

func (client *JSONAPI) Lookup(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL, nil)
	if err != nil {
		return nil, err
	}
	query := request.URL.Query()
	query.Set("name", name)
	query.Set("type", dns.Type(qtype).String())
	request.URL.RawQuery = query.Encode()

	response, err := client.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 512<<10))
	if err != nil {
		return nil, err
	}
	var payload JSONAPIResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	message, err := payload.toDNSMsg()
	if err != nil {
		return message, err
	}
	dnsRequest := new(dns.Msg).SetQuestion(dns.Fqdn(name), qtype)
	message, err = validateResponse(dnsRequest, message, client.baseURL)
	if err != nil {
		return message, err
	}
	if payload.Error != "" {
		return message, fmt.Errorf("dns json api: %s", payload.Error)
	}
	return message, nil
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

// toDNSMsg maps only fields represented by the JSON DNS response.
func (response JSONAPIResponse) toDNSMsg() (*dns.Msg, error) {
	message := &dns.Msg{MsgHdr: dns.MsgHdr{
		Response:           true,
		Truncated:          response.TC,
		RecursionDesired:   response.RD,
		RecursionAvailable: response.RA,
		AuthenticatedData:  response.AD,
		CheckingDisabled:   response.CD,
		Rcode:              response.Status,
	}}

	for _, question := range response.Question {
		name, err := validatedDNSName(question.Name)
		if err != nil {
			return message, err
		}
		message.Question = append(message.Question, dns.Question{
			Name:   name,
			Qtype:  question.Type,
			Qclass: dns.ClassINET,
		})
	}

	for _, answer := range response.Answer {
		record, err := answer.toRR()
		if err != nil {
			return message, err
		}
		message.Answer = append(message.Answer, record)
	}
	for _, authority := range response.Authority {
		record, err := authority.toRR()
		if err != nil {
			return message, err
		}
		message.Ns = append(message.Ns, record)
	}
	return message, nil
}

var _ json.Unmarshaler = (*JSONAPIQuestions)(nil)

type JSONAPIQuestions []JSONAPIQuestion

func (questions *JSONAPIQuestions) UnmarshalJSON(data []byte) error {
	var values []JSONAPIQuestion
	if err := json.Unmarshal(data, &values); err == nil {
		*questions = values
		return nil
	}
	var single JSONAPIQuestion
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*questions = []JSONAPIQuestion{single}
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

// toRR delegates RDATA parsing to miekg/dns while isolating untrusted owner text.
func (answer JSONAPIAnswer) toRR() (dns.RR, error) {
	name, err := validatedDNSName(answer.Name)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(answer.Data, "\r\n") {
		return nil, fmt.Errorf("invalid DNS record data: contains a newline")
	}

	recordText := fmt.Sprintf(". %d IN %s %s", answer.TTL, dns.Type(answer.Type).String(), answer.Data)
	record, err := dns.NewRR(recordText)
	if err != nil {
		return nil, fmt.Errorf("parse DNS record %q: %w", answer.Name, err)
	}
	if record == nil {
		return nil, fmt.Errorf("parse DNS record %q: no record", answer.Name)
	}
	header := record.Header()
	if header.Rrtype != answer.Type || header.Class != dns.ClassINET || header.Ttl != answer.TTL {
		return nil, fmt.Errorf("parse DNS record %q: header mismatch", answer.Name)
	}
	header.Name = name
	return record, nil
}

// validatedDNSName rejects multiline input before canonicalizing a DNS owner name.
func validatedDNSName(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\r\n") {
		return "", fmt.Errorf("invalid DNS name %q", name)
	}
	canonical := dns.Fqdn(name)
	if _, ok := dns.IsDomainName(canonical); !ok {
		return "", fmt.Errorf("invalid DNS name %q", name)
	}
	return canonical, nil
}
