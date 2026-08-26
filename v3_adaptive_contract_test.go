package fastresolver_test

import (
	"context"
	"errors"
	"testing"
	"time"

	fastresolver "github.com/lixiangzhong/fastresolver/v3"
	"github.com/miekg/dns"
)

type blockingResolver struct{}

func (blockingResolver) Lookup(
	ctx context.Context,
	_ string,
	_ uint16,
) (*dns.Msg, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestV3_AdaptivePoolDecreasesCapacityAfterDeadline(t *testing.T) {
	pool, err := fastresolver.NewAdaptivePoolResolver(
		[]fastresolver.ILookup{blockingResolver{}},
		fastresolver.WithAdaptiveInitialQPS(20),
		fastresolver.WithAdaptiveDecreaseFactor(0.5),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = pool.Lookup(ctx, "timeout.example", dns.TypeA)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got error %v, want context deadline exceeded", err)
	}
	stats := pool.Stats()
	if len(stats) != 1 {
		t.Fatalf("got %d upstream stats, want 1", len(stats))
	}
	if stat := stats[0]; stat.EstimatedQPS != 10 || stat.Failures != 1 || stat.InFlight != 0 {
		t.Fatalf("unexpected upstream stats after deadline: %+v", stat)
	}
}
