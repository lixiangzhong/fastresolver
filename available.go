package fastresolver

import "sync/atomic"

type StatefulResolver interface {
	Resolver
	Stateful
}

type Stateful interface {
	Available() bool
	Record(success bool)
}

func NewStatefulResolver(r Resolver, threshold uint64) StatefulResolver {
	return &statefulResolver{
		Resolver: r,
		Stateful: &baseAvailable{threshold: threshold, failed: &atomic.Uint64{}, success: &atomic.Uint64{}},
	}
}

type statefulResolver struct {
	Resolver
	Stateful
}

var _ Stateful = (*baseAvailable)(nil)

type baseAvailable struct {
	threshold uint64
	failed    *atomic.Uint64
	success   *atomic.Uint64
}

// Available implements Available.
func (b *baseAvailable) Available() bool {
	success := b.success.Load()
	failed := b.failed.Load()
	total := success + failed
	if total < b.threshold {
		return true
	}
	if success == 0 {
		return false
	}
	return success > failed
}

// Record implements Available.
func (b *baseAvailable) Record(success bool) {
	if success {
		b.success.Add(1)
	} else {
		b.failed.Add(1)
	}
}
