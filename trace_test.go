package fastresolver

import (
	"context"
	"testing"
)

func TestFallbackTrace_LookupIP(t *testing.T) {
	ctx := context.Background()
	name := "8d23gmsz.top"
	f := FallbackTrace{Resolver: Upstream{Addr: "1.1.1.1"}}
	ret, err := f.LookupIP(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(ret)
}
