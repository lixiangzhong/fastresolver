package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestDefault(t *testing.T) {
	r := Default()
	rr, err := r.Lookup(context.Background(), "wubu.gov.cn", dns.TypeAAAA)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", rr)
}
