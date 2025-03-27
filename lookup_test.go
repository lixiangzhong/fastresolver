package fastresolver

import (
	"context"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestResolver_Lookup(t *testing.T) {

	tmp := `1.0.0.1
1.1.1.1
134.195.4.2
149.112.112.112
159.89.120.99
185.228.168.9
185.228.169.9
195.46.39.39
195.46.39.40
205.171.2.65
205.171.3.65
208.67.220.220
208.67.222.222
216.146.35.35
216.146.36.36
64.6.64.6
64.6.65.6
74.82.42.42
76.76.10.0
76.76.2.0
77.88.8.1
77.88.8.8
8.20.247.20
8.26.56.26
8.8.4.4
8.8.8.8
84.200.69.80
84.200.70.40
89.233.43.71
9.9.9.9
91.239.100.100`
	for _, addr := range strings.Fields(tmp) {
		r, err := NewResolver(addr)
		if err != nil {
			t.Log(addr, err)
			continue
		}
		rr, err := r.Lookup(context.Background(), "cl.app", dns.TypeNS)
		if err != nil {
			t.Log(addr, err)
			continue
		}
		if rr.NXDomain {
			t.Log("NXDomain", addr)
		}
	}
}
