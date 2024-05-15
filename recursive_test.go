package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestRecursiveLookup(t *testing.T) {
	rr, err := RecursiveLookup(context.Background(), "www.baidu.com", dns.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#+v", rr)
}
