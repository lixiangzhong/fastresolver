// Package fastresolver 提供了一个基于声明式构建器（Builder Pattern）和洋葱模型（Onion Middleware Model）的 DNS 解析装配机制。
//
// 通过使用 Decorator（装饰器/中间件）链路，开发者可以按预期的执行顺序，灵活地组装各种不同的解析器行为，
// 例如：超时控制、失败重试、LRU 缓存拦截、CNAME 自动追踪、失败熔断和限速保护等。
//
// 在洋葱模型中，最先传入的装饰器在最外层（最先执行），最后传入的在最内层（最后执行，紧邻基础解析器）。
package fastresolver

import (
	"fmt"
	"time"
)

// Decorator 表示一个中间件装饰器函数（洋葱模型设计）。
// 它接收一个内层的 ILookup 解析器，并返回一个包裹了特定功能（如缓存、限速、熔断等）后的外层 ILookup 解析器。
// 通过将多个 Decorator 嵌套组合，可以实现链式的行为流控制。
type Decorator func(ILookup) ILookup

// WithRateLimit 返回一个为解析器添加 QPS 限速保护的装饰器。
func WithRateLimit(qps int) Decorator {
	return func(next ILookup) ILookup {
		return NewRateLimitResolver(next, qps)
	}
}

// WithCircuitBreaker 返回一个为解析器添加失败熔断保护的装饰器（默认冷却期为 10 秒）。
func WithCircuitBreaker(threshold uint64) Decorator {
	return func(next ILookup) ILookup {
		return NewCircuitBreakerResolver(next, threshold)
	}
}

// WithCircuitBreakerCooling 返回一个支持自定义冷却时间的熔断器装饰器。
func WithCircuitBreakerCooling(threshold uint64, coolingTimeout time.Duration) Decorator {
	return func(next ILookup) ILookup {
		return NewCircuitBreakerResolverWithCooling(next, threshold, coolingTimeout)
	}
}

// WithRetry 返回一个为解析器添加失败重试控制的装饰器。
func WithRetry(count int) Decorator {
	return func(next ILookup) ILookup {
		return NewRetryResolver(count, next)
	}
}

// WithCacheResolver 返回一个为解析器添加 LRU 缓存读写能力的装饰器。
func WithCacheResolver(cache Cache) Decorator {
	return func(next ILookup) ILookup {
		return NewCacheResolver(cache, next)
	}
}

// WithFollowCname 返回一个支持自动追踪递归跟随 CNAME 记录的装饰器。
func WithFollowCname() Decorator {
	return func(next ILookup) ILookup {
		return NewFollowCnameResolver(next)
	}
}

// WithTimeoutResolver 返回一个在 context 中统一注入超时限制的装饰器。
func WithTimeoutResolver(timeout time.Duration) Decorator {
	return func(next ILookup) ILookup {
		return NewTimeoutResolver(next, timeout)
	}
}

// NewCustomResolverFromBase 使用中间件装饰器链对一个基础解析器进行包卷。
// 装饰器的应用顺序是“由内向外”（从右往左）：
// 传入的 decorators 列表中，decorators[0] 会在最外层（最先被执行调用），
// decorators[len-1] 会在最内层（直接包裹 base 节点，最后被执行）。
func NewCustomResolverFromBase(base ILookup, decorators ...Decorator) ILookup {
	resolver := base
	// 从最后一个装饰器向第一个依次倒序应用，实现最先传入的在最外层执行
	for i := len(decorators) - 1; i >= 0; i-- {
		resolver = decorators[i](resolver)
	}
	return resolver
}

// NewCustomResolver 根据上游解析器地址列表构建一个复合解析器。
// 如果传入了多个上游地址，默认内部会自动用 RandomBalancer 进行多服务器随机负载均衡。
// 装饰器的应用顺序是“由内向外”（从右往左）：
// 传入的 decorators 列表中，decorators[0] 会在最外层（最先被执行调用）。
func NewCustomResolver(servers []string, decorators ...Decorator) (ILookup, error) {
	var resolvers []ILookup
	// 构建底层的每一个单点 Resolver
	for _, addr := range servers {
		r, err := NewResolver(addr)
		if err != nil {
			return nil, err
		}
		resolvers = append(resolvers, r)
	}

	if len(resolvers) == 0 {
		return nil, fmt.Errorf("no base resolvers created")
	}

	// 组合底层的解析节点，支持负载均衡
	var base ILookup
	if len(resolvers) > 1 {
		base = NewLoadBalanceResolver(NewRandomBalancer(), resolvers...)
	} else {
		base = resolvers[0]
	}

	// 流式应用装饰器链条
	return NewCustomResolverFromBase(base, decorators...), nil
}
