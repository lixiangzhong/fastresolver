package fastresolver

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

var _ ILookup = (*MetricsResolver)(nil)

// MetricsResolver 是一个带有监控指标计数功能的解析器包装器。
// 它包装了一个底层的 ILookup 实例，在 Lookup 请求执行时自动统计成功和失败的次数。
type MetricsResolver struct {
	resolver ILookup
	success  *atomic.Uint64
	failure  *atomic.Uint64
}

// NewMetricsResolver 创建并初始化一个 MetricsResolver。
func NewMetricsResolver(resolver ILookup) *MetricsResolver {
	return &MetricsResolver{
		resolver: resolver,
		success:  new(atomic.Uint64),
		failure:  new(atomic.Uint64),
	}
}

// Lookup 实现了 ILookup 接口。执行底层解析并更新成功/失败计数器。
func (m *MetricsResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	ret, err := m.resolver.Lookup(ctx, name, qtype)
	if err != nil {
		m.failure.Add(1)
	} else {
		m.success.Add(1)
	}
	return ret, err
}

// CircuitBreaker 定义了熔断器准入状态的判断接口。
type CircuitBreaker interface {
	Accept() bool
}

var _ CircuitBreaker = (*CircuitBreakerResolver)(nil)
var _ ILookup = (*CircuitBreakerResolver)(nil)

// State 表示熔断器的状态类型（三态状态机）。
type State int32

const (
	// StateClosed 表示正常工作状态，所有请求都会放行传递给底层 Resolver。
	StateClosed State = iota
	// StateOpen 表示熔断状态，拒绝所有外部查询，直接快速返回 ErrCircuitBreaker 错误。
	StateOpen
	// StateHalfOpen 表示半开状态，允许单个试探流量通过，用以判断底层解析服务是否已自愈。
	StateHalfOpen
)

// CircuitBreakerResolver 是基于三态状态机的熔断器解析器包装器。
// 它通过对底层失败指标进行监控，在连续失败达到阈值时触发熔断，并在冷却超时后自动通过半开状态探测进行服务自愈。
type CircuitBreakerResolver struct {
	resolver         *MetricsResolver // 带有统计指标的底层解析器
	failureThreshold uint64           // 触发熔断的连续失败次数阈值
	coolingTimeout   time.Duration    // 触发熔断后，等待自愈探测的冷却时间

	state           int32 // 状态机的当前状态，对应 State，使用原子操作管理以保证并发安全
	lastStateChange time.Time
	mu              sync.RWMutex // 保护非原子的 lastStateChange 时间戳
}

// NewCircuitBreakerResolver 创建一个默认的熔断器解析器。
// 默认分配 10 秒的熔断冷却时间。
func NewCircuitBreakerResolver(resolver ILookup, failureThreshold uint64) *CircuitBreakerResolver {
	return NewCircuitBreakerResolverWithCooling(resolver, failureThreshold, 10*time.Second)
}

// NewCircuitBreakerResolverWithCooling 创建一个支持自定义冷却超时的熔断器解析器。
func NewCircuitBreakerResolverWithCooling(resolver ILookup, failureThreshold uint64, coolingTimeout time.Duration) *CircuitBreakerResolver {
	return &CircuitBreakerResolver{
		resolver:         NewMetricsResolver(resolver),
		failureThreshold: failureThreshold,
		coolingTimeout:   coolingTimeout,
		state:            int32(StateClosed),
		lastStateChange:  time.Now(),
	}
}

// getState 获取当前熔断器的原子状态。
func (c *CircuitBreakerResolver) getState() State {
	return State(atomic.LoadInt32(&c.state))
}

// compareAndSwapState 原子地进行状态转换，并刷新最近状态变更时间戳（用于从 Open 抢占 HalfOpen 状态）。
func (c *CircuitBreakerResolver) compareAndSwapState(oldState, newState State) bool {
	swapped := atomic.CompareAndSwapInt32(&c.state, int32(oldState), int32(newState))
	if swapped {
		c.mu.Lock()
		c.lastStateChange = time.Now()
		c.mu.Unlock()
	}
	return swapped
}

// changeState 强制进行状态转换，如果是恢复为 Closed 状态，则一并重置底层指标计数器。
func (c *CircuitBreakerResolver) changeState(oldState, newState State) {
	if atomic.CompareAndSwapInt32(&c.state, int32(oldState), int32(newState)) {
		c.mu.Lock()
		c.lastStateChange = time.Now()
		c.mu.Unlock()
		if newState == StateClosed {
			// 自愈恢复成功，清空历史统计指标，重新开始统计
			c.resolver.failure.Store(0)
			c.resolver.success.Store(0)
		}
	}
}

// Lookup 实现了 ILookup 接口。其内部实现了熔断器的拦截、状态判断与流转自愈逻辑。
func (c *CircuitBreakerResolver) Lookup(ctx context.Context, name string, qtype uint16) (DNSRR, error) {
	state := c.getState()

	// 1. 如果处于 Open (熔断) 状态，判断冷却时间是否已到，以尝试切入 HalfOpen (半开) 进行健康探测
	if state == StateOpen {
		c.mu.RLock()
		coolingOver := time.Since(c.lastStateChange) >= c.coolingTimeout
		c.mu.RUnlock()

		if coolingOver {
			// 冷却时间已到，原子地尝试将状态改为 HalfOpen 允许放行单个试探请求
			if c.compareAndSwapState(StateOpen, StateHalfOpen) {
				state = StateHalfOpen
			} else {
				// 如果在 CAS 竞争中落败，代表其他并发协程已经抢先进行了状态流转，重新获取当前状态
				state = c.getState()
			}
		} else {
			// 仍在冷却期内，快速失败，直接返回熔断错误
			return DNSRR{}, ErrCircuitBreaker
		}
	}

	// 2. 双重检查：如果现在状态依然是 Open（例如抢占 HalfOpen 失败），则直接拦截返回
	if state == StateOpen {
		return DNSRR{}, ErrCircuitBreaker
	}

	// 3. 执行底层的真实解析请求
	ret, err := c.resolver.Lookup(ctx, name, qtype)

	// 4. 根据本次调用的结果，决定状态机的状态流转
	if err != nil {
		if state == StateHalfOpen {
			// 半开探测状态下依然遭遇失败，表示底层服务尚未恢复，重新跌入 Open 状态，并刷新冷却定时器
			c.changeState(StateHalfOpen, StateOpen)
		} else {
			// Closed (正常) 状态下失败，检查是否累计失败次数已经达到熔断临界点
			if c.resolver.failure.Load() >= c.failureThreshold {
				c.changeState(StateClosed, StateOpen)
			}
		}
	} else {
		if state == StateHalfOpen {
			// 半开探测状态下请求成功，表示底层服务已经自愈恢复，状态变更为 Closed 并重置计数器
			c.changeState(StateHalfOpen, StateClosed)
		} else if state == StateClosed {
			// 正常成功调用时，重置连续失败计数器为 0，使 failure 计数代表连续失败
			c.resolver.failure.Store(0)
		}
	}

	return ret, err
}

// Accept 实现了 CircuitBreaker 接口。由负载均衡器调用，用于判断解析器目前是否可用。
func (c *CircuitBreakerResolver) Accept() bool {
	state := c.getState()
	if state == StateClosed {
		return true
	}
	if state == StateOpen {
		// 在 Open 状态下，如果冷却时间到了，允许负载均衡器分发试探流量（Accept 为 true 会流转到 HalfOpen）
		c.mu.RLock()
		coolingOver := time.Since(c.lastStateChange) >= c.coolingTimeout
		c.mu.RUnlock()
		return coolingOver
	}
	// HalfOpen 状态支持试探探测，返回 true
	return true
}
