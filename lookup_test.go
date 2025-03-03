package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestResolver_Lookup(t *testing.T) {
	r, err := NewResolver("1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	rr, err := r.Lookup(context.Background(), "haotv.net", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#+v", rr)
}
