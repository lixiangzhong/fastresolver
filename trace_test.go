package fastresolver

import (
	"context"
	"testing"
)

func TestFallbackTrace_LookupIP(t *testing.T) {
	ctx := context.Background()
	name := "www.qq.com"
	f := FallbackTrace{Resolver: Upstream{Addr: "127.0.0.1"}}
	ret, err := f.LookupIP(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(ret)
}
