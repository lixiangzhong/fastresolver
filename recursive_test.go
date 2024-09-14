package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestRecursiveLookup(t *testing.T) {
	rr, err := RecursiveLookup(context.Background(), "baidu.com", dns.TypeNS)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#+v", rr)
}
