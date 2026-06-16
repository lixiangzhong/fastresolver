package fastresolver_test

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/lixiangzhong/fastresolver/v2"
	"github.com/miekg/dns"
)

// ExampleNewCustomResolver 展示了如何使用声明式构建器 (Functional Options / Builder Pattern)
// 以及洋葱模型装饰器链来组装一个自定义的 DNS 解析器。
//
// 该解析器集成了：
// 1. LRU 缓存拦截层 (Cache)
// 2. 超时控制拦截层 (Timeout)
// 3. 失败重试拦截层 (Retry)
//
// 拦截链的执行流向（从外向内）：
// 客户端调用 Lookup() -> [最外层] 缓存拦截器 -> [中层] 超时拦截器 -> [最内层] 重试拦截器 -> 负载均衡/基础解析器。
func ExampleNewCustomResolver() {
	// -------------------------------------------------------------------------
	// 1. 搭建本地 Mock UDP DNS 服务器，避免对公网的依赖，从而保证测试的稳定性和可重复性。
	// -------------------------------------------------------------------------
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("监听本地UDP端口失败: %v\n", err)
		return
	}

	server := &dns.Server{
		PacketConn: pc,
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			m := new(dns.Msg)
			m.SetReply(r)
			m.Authoritative = true
			if len(r.Question) > 0 {
				q := r.Question[0]
				// 模拟解析 "example.com" 的 A 记录，返回固定 IP 192.0.2.1
				if q.Name == "example.com." && q.Qtype == dns.TypeA {
					a := &dns.A{
						Hdr: dns.RR_Header{
							Name:   q.Name,
							Rrtype: dns.TypeA,
							Class:  dns.ClassINET,
							Ttl:    60, // 缓存 TTL 为 60 秒
						},
						A: net.ParseIP("192.0.2.1"),
					}
					m.Answer = append(m.Answer, a)
				}
			}
			_ = w.WriteMsg(m)
		}),
	}

	// 启动 DNS 服务
	go func() {
		_ = server.ActivateAndServe()
	}()
	defer server.Shutdown()

	// -------------------------------------------------------------------------
	// 2. 声明式地构建和包装自定义解析器。
	// -------------------------------------------------------------------------
	// 获取 Mock 服务器的监听地址作为上游 DNS 地址
	serverAddr := server.PacketConn.LocalAddr().String()
	servers := []string{serverAddr}

	// 初始化一个容量为 100 并且默认缓存过期时间为 1 分钟的 LRU 缓存
	cache := fastresolver.NewLRU(100, time.Minute)

	// 使用声明式 Functional Options 组装中间件。
	// 链的传入顺序就是从外向内的包装和调用执行顺序。
	resolver, err := fastresolver.NewCustomResolver(
		servers,
		fastresolver.WithCacheResolver(cache),                  // 最外层：优先读取/写入缓存，避免不必要的网络往返
		fastresolver.WithTimeoutResolver(200*time.Millisecond), // 中间层：限制单次查询在 200 毫秒内完成
		fastresolver.WithRetry(2),                              // 最内层：如果单次查询因超时或网络抖动失败，重试最多 2 次
	)
	if err != nil {
		fmt.Printf("构建自定义解析器失败: %v\n", err)
		return
	}

	// -------------------------------------------------------------------------
	// 3. 验证解析行为与缓存机制。
	// -------------------------------------------------------------------------
	ctx := context.Background()

	// 第一次查询：由于缓存中没有数据，此请求会穿透缓存并调用上游 Mock DNS 服务，然后将结果写入缓存。
	res1, err := resolver.Lookup(ctx, "example.com", dns.TypeA)
	if err != nil {
		fmt.Printf("第一次解析失败: %v\n", err)
		return
	}
	fmt.Printf("第一次解析成功，IP 列表 = %v\n", res1.A)

	// 第二次查询：此时在最外层的 WithCacheResolver 拦截中，能直接命中 LRU 缓存，直接返回而不会再产生网络请求。
	res2, err := resolver.Lookup(ctx, "example.com", dns.TypeA)
	if err != nil {
		fmt.Printf("第二次解析失败: %v\n", err)
		return
	}
	fmt.Printf("第二次解析成功，IP 列表 = %v\n", res2.A)

	// Output:
	// 第一次解析成功，IP 列表 = [192.0.2.1]
	// 第二次解析成功，IP 列表 = [192.0.2.1]
}
