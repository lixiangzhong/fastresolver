package fastresolver

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestJSONAPI_Lookup(t *testing.T) {

	// r := NewJSONAPI("https://doh.pub/resolve", time.Second*3)
	r := NewJSONAPI("https://dns.alidns.com/resolve", time.Second*3)

	rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#+v", rr)
}
