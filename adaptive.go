package fastresolver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// ErrAdaptiveUpstream indicates that an upstream returned a retryable DNS response.
var ErrAdaptiveUpstream = errors.New("adaptive upstream unavailable")

// AdaptivePoolOptions controls feedback-based upstream rate discovery.
type AdaptivePoolOptions struct {
	// InitialQPS is the initial estimated capacity of each upstream.
	InitialQPS float64

	// MinQPS is the lowest estimated capacity after failures.
	MinQPS float64

	// MaxQPS is the highest estimated capacity allowed during discovery.
	MaxQPS float64

	// IncreasePerSecond is the additive QPS increase after a second of successes.
	IncreasePerSecond float64

	// DecreaseFactor multiplies the estimated QPS after a retryable failure.
	DecreaseFactor float64

	// FailureCooldown temporarily removes a failed upstream from selection.
	FailureCooldown time.Duration

	// MaxAttempts is the maximum number of distinct upstreams tried per lookup.
	MaxAttempts int
}

// AdaptivePoolOption customizes AdaptivePoolResolver construction.
type AdaptivePoolOption func(options *AdaptivePoolOptions)

// WithAdaptiveInitialQPS sets the initial estimated capacity of each upstream.
func WithAdaptiveInitialQPS(qps float64) AdaptivePoolOption {
	return func(options *AdaptivePoolOptions) { options.InitialQPS = qps }
}

// WithAdaptiveMinQPS sets the lowest estimated capacity after failures.
func WithAdaptiveMinQPS(qps float64) AdaptivePoolOption {
	return func(options *AdaptivePoolOptions) { options.MinQPS = qps }
}

// WithAdaptiveMaxQPS sets the highest capacity allowed during discovery.
func WithAdaptiveMaxQPS(qps float64) AdaptivePoolOption {
	return func(options *AdaptivePoolOptions) { options.MaxQPS = qps }
}

// WithAdaptiveIncreasePerSecond sets the additive QPS increase rate.
func WithAdaptiveIncreasePerSecond(qps float64) AdaptivePoolOption {
	return func(options *AdaptivePoolOptions) { options.IncreasePerSecond = qps }
}

// WithAdaptiveDecreaseFactor sets the multiplicative decrease factor.
func WithAdaptiveDecreaseFactor(factor float64) AdaptivePoolOption {
	return func(options *AdaptivePoolOptions) { options.DecreaseFactor = factor }
}

// WithAdaptiveFailureCooldown sets how long a failed upstream is excluded.
func WithAdaptiveFailureCooldown(cooldown time.Duration) AdaptivePoolOption {
	return func(options *AdaptivePoolOptions) { options.FailureCooldown = cooldown }
}

// WithAdaptiveMaxAttempts sets the number of distinct upstreams tried per lookup.
func WithAdaptiveMaxAttempts(attempts int) AdaptivePoolOption {
	return func(options *AdaptivePoolOptions) { options.MaxAttempts = attempts }
}

// AdaptiveUpstreamStats is a point-in-time snapshot of one upstream's state.
type AdaptiveUpstreamStats struct {
	// EstimatedQPS is the current learned request rate.
	EstimatedQPS float64

	// Successes is the number of successful responses observed by the pool.
	Successes uint64

	// Failures is the number of retryable failures observed by the pool.
	Failures uint64

	// InFlight is the number of requests currently executing on the upstream.
	InFlight int
}

type adaptiveUpstream struct {
	resolver ILookup

	mu            sync.Mutex
	rate          float64
	nextAllowed   time.Time
	cooldownUntil time.Time
	successes     uint64
	failures      uint64
	inFlight      int
}

// AdaptivePoolResolver learns upstream capacity from response feedback.
type AdaptivePoolResolver struct {
	options   AdaptivePoolOptions
	upstreams []*adaptiveUpstream

	selectionMu sync.Mutex
	cursor      atomic.Uint64
}

var _ ILookup = (*AdaptivePoolResolver)(nil)

// NewAdaptivePoolResolver creates a feedback-based pool over resolvers.
func NewAdaptivePoolResolver(
	resolvers []ILookup,
	opts ...AdaptivePoolOption,
) (*AdaptivePoolResolver, error) {
	if len(resolvers) == 0 {
		return nil, ErrNoResolver
	}
	options := defaultAdaptivePoolOptions(len(resolvers))
	for optionIndex, apply := range opts {
		if apply == nil {
			return nil, fmt.Errorf("adaptive pool option at index %d is nil", optionIndex)
		}
		apply(&options)
	}
	if err := validateAdaptivePoolOptions(options); err != nil {
		return nil, err
	}
	options.MaxAttempts = min(options.MaxAttempts, len(resolvers))

	pool := &AdaptivePoolResolver{
		options:   options,
		upstreams: make([]*adaptiveUpstream, 0, len(resolvers)),
	}
	for resolverIndex, resolver := range resolvers {
		if resolver == nil {
			return nil, fmt.Errorf("resolver at index %d is nil", resolverIndex)
		}
		pool.upstreams = append(pool.upstreams, &adaptiveUpstream{
			resolver: resolver,
			rate:     options.InitialQPS,
		})
	}
	return pool, nil
}

// Lookup implements ILookup and retries retryable failures on distinct upstreams.
func (pool *AdaptivePoolResolver) Lookup(
	ctx context.Context,
	name string,
	qtype uint16,
) (*dns.Msg, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	excluded := make([]bool, len(pool.upstreams))
	var failures []error
	var lastResponse *dns.Msg
	for attempt := 0; attempt < pool.options.MaxAttempts; {
		upstreamIndex, wait := pool.reserveUpstream(excluded)
		if upstreamIndex < 0 {
			if err := waitForAdaptiveSlot(ctx, wait); err != nil {
				return lastResponse, err
			}
			continue
		}
		attempt++

		upstream := pool.upstreams[upstreamIndex]
		response, err := normalizeLookupResult(upstream.resolver.Lookup(ctx, name, qtype))
		if ctxErr := ctx.Err(); ctxErr != nil {
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				upstream.observeFailure(pool.options)
			} else {
				upstream.release()
			}
			return response, ctxErr
		}

		failure := adaptiveFailure(response, err)
		if failure == nil {
			upstream.observeSuccess(pool.options)
			return response, nil
		}

		upstream.observeFailure(pool.options)
		excluded[upstreamIndex] = true
		lastResponse = response
		failures = append(failures, failure)
	}

	return lastResponse, errors.Join(failures...)
}

// Stats returns snapshots in the same order as the constructor's resolvers.
func (pool *AdaptivePoolResolver) Stats() []AdaptiveUpstreamStats {
	stats := make([]AdaptiveUpstreamStats, len(pool.upstreams))
	for upstreamIndex, upstream := range pool.upstreams {
		upstream.mu.Lock()
		stats[upstreamIndex] = AdaptiveUpstreamStats{
			EstimatedQPS: upstream.rate,
			Successes:    upstream.successes,
			Failures:     upstream.failures,
			InFlight:     upstream.inFlight,
		}
		upstream.mu.Unlock()
	}
	return stats
}

// defaultAdaptivePoolOptions returns the zero-configuration controller policy.
func defaultAdaptivePoolOptions(upstreamCount int) AdaptivePoolOptions {
	return AdaptivePoolOptions{
		InitialQPS:        10,
		MinQPS:            1,
		MaxQPS:            1_000,
		IncreasePerSecond: 5,
		DecreaseFactor:    0.7,
		FailureCooldown:   100 * time.Millisecond,
		MaxAttempts:       min(3, upstreamCount),
	}
}

// validateAdaptivePoolOptions validates the final policy after all options apply.
func validateAdaptivePoolOptions(options AdaptivePoolOptions) error {
	switch {
	case !isFinitePositive(options.InitialQPS):
		return fmt.Errorf("initial QPS must be positive")
	case !isFinitePositive(options.MinQPS):
		return fmt.Errorf("minimum QPS must be positive")
	case !isFinitePositive(options.MaxQPS):
		return fmt.Errorf("maximum QPS must be positive")
	case options.MaxQPS < options.MinQPS:
		return fmt.Errorf("maximum QPS %.2f is below minimum %.2f", options.MaxQPS, options.MinQPS)
	case options.InitialQPS < options.MinQPS || options.InitialQPS > options.MaxQPS:
		return fmt.Errorf("initial QPS %.2f is outside range %.2f..%.2f", options.InitialQPS, options.MinQPS, options.MaxQPS)
	case !isFinitePositive(options.IncreasePerSecond):
		return fmt.Errorf("increase per second must be positive")
	case !isFinitePositive(options.DecreaseFactor) || options.DecreaseFactor >= 1:
		return fmt.Errorf("decrease factor must be between 0 and 1")
	case options.FailureCooldown < 0:
		return fmt.Errorf("failure cooldown must not be negative")
	case options.MaxAttempts <= 0:
		return fmt.Errorf("maximum attempts must be positive")
	}
	return nil
}

// isFinitePositive reports whether value can safely be used in rate arithmetic.
func isFinitePositive(value float64) bool {
	return value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

// reserveUpstream claims an immediately available slot or returns the shortest wait.
func (pool *AdaptivePoolResolver) reserveUpstream(excluded []bool) (int, time.Duration) {
	pool.selectionMu.Lock()
	defer pool.selectionMu.Unlock()

	now := time.Now()
	start := int(pool.cursor.Add(1)-1) % len(pool.upstreams)
	selected := -1
	selectedInFlight := int(^uint(0) >> 1)
	var earliest time.Time
	for offset := 0; offset < len(pool.upstreams); offset++ {
		upstreamIndex := (start + offset) % len(pool.upstreams)
		if excluded[upstreamIndex] {
			continue
		}
		upstream := pool.upstreams[upstreamIndex]
		upstream.mu.Lock()
		availableAt := upstream.nextAllowed
		if upstream.cooldownUntil.After(availableAt) {
			availableAt = upstream.cooldownUntil
		}
		inFlight := upstream.inFlight
		upstream.mu.Unlock()

		if availableAt.After(now) {
			if earliest.IsZero() || availableAt.Before(earliest) {
				earliest = availableAt
			}
			continue
		}
		if selected < 0 || inFlight < selectedInFlight {
			selected = upstreamIndex
			selectedInFlight = inFlight
		}
	}
	if selected < 0 {
		if earliest.IsZero() {
			return -1, time.Millisecond
		}
		return -1, max(time.Until(earliest), time.Millisecond)
	}

	upstream := pool.upstreams[selected]
	upstream.mu.Lock()
	upstream.nextAllowed = now.Add(intervalForQPS(upstream.rate))
	upstream.inFlight++
	upstream.mu.Unlock()
	return selected, 0
}

// release drops an in-flight request without changing capacity feedback.
func (upstream *adaptiveUpstream) release() {
	upstream.mu.Lock()
	upstream.inFlight--
	upstream.mu.Unlock()
}

// observeSuccess performs the additive-increase half of AIMD.
func (upstream *adaptiveUpstream) observeSuccess(options AdaptivePoolOptions) {
	upstream.mu.Lock()
	defer upstream.mu.Unlock()

	upstream.inFlight--
	upstream.successes++
	increase := options.IncreasePerSecond / upstream.rate
	upstream.rate = min(options.MaxQPS, upstream.rate+increase)
}

// observeFailure performs multiplicative decrease and starts a cooldown.
func (upstream *adaptiveUpstream) observeFailure(options AdaptivePoolOptions) {
	upstream.mu.Lock()
	defer upstream.mu.Unlock()

	upstream.inFlight--
	upstream.failures++
	upstream.rate = max(options.MinQPS, upstream.rate*options.DecreaseFactor)
	upstream.cooldownUntil = time.Now().Add(options.FailureCooldown)
}

// adaptiveFailure classifies transport errors and QoS-like DNS response codes.
func adaptiveFailure(response *dns.Msg, err error) error {
	if err != nil {
		return err
	}
	if response == nil {
		return ErrNoResponse
	}
	switch response.Rcode {
	case dns.RcodeRefused, dns.RcodeServerFailure:
		return fmt.Errorf("%w: %s", ErrAdaptiveUpstream, dns.RcodeToString[response.Rcode])
	default:
		return nil
	}
}

// intervalForQPS converts a learned rate into a minimum dispatch interval.
func intervalForQPS(qps float64) time.Duration {
	interval := time.Duration(float64(time.Second) / qps)
	return max(interval, time.Nanosecond)
}

// waitForAdaptiveSlot waits without losing caller cancellation.
func waitForAdaptiveSlot(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
