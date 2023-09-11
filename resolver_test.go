package fastresolver

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestUpstream_LookupIP(t *testing.T) {
	u := Upstream{
		Addr: "1.1.1.1",
	}
	answer, err := u.LookupIP(context.Background(), "www.qq.com")
	t.Log(answer, err)
}

func TestUpstream_LookupNS(t *testing.T) {
	u := Upstream{
		Addr: "1.1.1.1",
	}
	answer, err := u.LookupNS(context.Background(), "baidu.com")
	t.Log(answer, err)
}

func Test_trace(t *testing.T) {
	ret, err := trace(context.Background(), "www.qq.com", dns.TypeA)
	t.Log(ret, err)
}

func TestDefaultResovler(t *testing.T) {
	// us := Upstreams{{Addr: "114.114.114.114"}, {Addr: "1.1.1.1"}, {Addr: "8.8.8.8"}}
	// v, e := us.LookupIP(context.Background(), "www.baidu.com")
	// t.Log(v, e)
	for i := 0; i < 5; i++ {
		v, e := DefaultResovler.LookupIP(context.Background(), "www.baidu.com")
		t.Log(v, e)
	}
}
