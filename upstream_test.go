package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestUpstream_Lookup(t *testing.T) {
	u := Upstream{Addr: "1.1.1.1"}
	rr, err := u.Lookup(context.Background(), "8d23gmsz.top", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(rr)
}
