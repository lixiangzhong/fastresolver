package fastresolver

// unwrapper 定义了获取底层被装饰解析器的接口。
type unwrapper interface {
	Unwrap() ILookup
}

// getCircuitBreaker 递归解包 resolver 链，寻找并返回最内层的 CircuitBreaker 实例。
// 若解析器链中未包含任何熔断器，则返回 nil。
func getCircuitBreaker(resolver ILookup) CircuitBreaker {
	for r := resolver; r != nil; {
		if cb, ok := r.(CircuitBreaker); ok {
			return cb
		}
		if uw, ok := r.(unwrapper); ok {
			r = uw.Unwrap()
		} else {
			break
		}
	}
	return nil
}
