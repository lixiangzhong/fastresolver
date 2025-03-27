package fastresolver

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDoH_Lookup(t *testing.T) {
	r := NewDoH("https://dns.alidns.com/dns-query", time.Second*3)
	rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#+v", rr)
}
