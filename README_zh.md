# Beat

[![Go Reference](https://pkg.go.dev/badge/github.com/uniyakcom/beat.svg)](https://pkg.go.dev/github.com/uniyakcom/beat)
[![Go Report Card](https://goreportcard.com/badge/github.com/uniyakcom/beat)](https://goreportcard.com/report/github.com/uniyakcom/beat)

[English](README.md) | [中文](README_zh.md)

高性能 Go 事件总线 — 三预设架构，零分配，零 CAS，零外部依赖。

## 特性

- **零外部依赖**: 纯标准库实现，`go.mod` 无任何第三方 require
- **三预设架构**: Sync (同步直调) / Async (Per-P SPSC) / Flow (Pipeline 流处理)
- **零分配 Emit**: 全部三预设 0 B/op, 0 allocs/op
- **极致性能**: Async 高并发 26 ns/op (38M ops/s), Sync 单线程 10.5 ns/op (95M ops/s)
- **零 CAS 热路径**: Per-P SPSC ring，atomic Load/Store only (≈ 普通 MOV)
- **并发安全**: RCU 订阅管理 + CoW 快照
- **模式匹配**: 支持通配符 `*`（单层）和 `**`（多层）
- **简洁 API**: `ForSync()` / `ForAsync()` / `ForFlow()`
- **包级便捷 API**: `beat.On()` / `beat.Emit()` 零配置直接使用（Sync 语义）

## 性能对比

测试环境：Intel Xeon E5-1650 v2 @ 3.50GHz (6C/12T)，Go 1.25.7

```bash
cd _benchmarks
go test -bench="." -benchmem -benchtime=3s -count=3 -run="^$" ./...
```

| 场景 | beat (Sync) | beat (Async) | EventBus | gookit/event |
|------|------------|-------------|----------|-------------|
| **单 handler 发布** | **11 ns** 0 alloc | 37 ns 0 alloc | 190 ns 0 alloc | 581 ns 2 alloc |
| **10 handler 发布** | **26 ns** 0 alloc | 34 ns 0 alloc | 1690 ns 1 alloc | 671 ns 2 alloc |
| **高并发 (Parallel)** | 29 ns 0 alloc | **27 ns** 0 alloc | 255 ns 0 alloc | 194 ns 2 alloc |

> 对比库：[asaskevich/EventBus](https://github.com/asaskevich/EventBus) (2k⭐) / [gookit/event](https://github.com/gookit/event) (565⭐)
>
> 完整对比代码见 [`_benchmarks/`](_benchmarks/) 目录（独立 go.mod，不污染主项目）

## 快速开始

### 安装

```bash
go get github.com/uniyakcom/beat
```

### 基本用法（包级 API — 最简方式）

```go
package main

import (
    "fmt"
    "github.com/uniyakcom/beat"
)

func main() {
    // 直接使用，无需创建 Bus，无需 Close
    beat.On("user.created", func(e *beat.Event) error {
        fmt.Printf("User: %s\n", string(e.Data))
        return nil
    })

    beat.Emit(&beat.Event{
        Type: "user.created",
        Data: []byte("alice"),
    })
}
```

> 包级 API 使用 Sync 语义（同步直调，支持 error 返回，无需 Close）。
> 需要 Async/Flow 时，使用实例化 API 👇

### 实例化用法（三预设任选）

```go
package main

import (
    "fmt"
    "github.com/uniyakcom/beat"
)

func main() {
    // 创建事件总线（三种方式任选）
    bus, _ := beat.ForAsync()       // 推荐：Per-P SPSC 高并发
    // bus, _ := beat.ForSync()     // 同步直调，error 返回
    // bus, _ := beat.ForFlow()     // Pipeline 流处理
    defer bus.Close()

    // 订阅事件
    id := bus.On("user.created", func(e *beat.Event) error {
        fmt.Printf("User: %s\n", string(e.Data))
        return nil
    })

    // 发布事件
    bus.Emit(&beat.Event{
        Type: "user.created",
        Data: []byte("alice"),
    })

    // 取消订阅
    bus.Off(id)
}
```

## 三预设选择

| 预设 | 适用场景 | 单线程延迟 | 高并发吞吐 | error 返回 | 生命周期 |
|------|----------|-----------|-----------|-----------|---------|
| **Sync** | RPC 调用、权限校验、同步钩子 | **10.5 ns** | ~38 ns | ✅ | 无需 Close |
| **Async** | 事件总线、日志聚合、实时推送 | 37 ns | **26 ns** | ❌ | 需 Close |
| **Flow** | ETL 流处理、批量数据加载 | **48 ns** | — | ❌ | 需 Close |

### API 快速参考

```go
// ===== 包级 API：零配置直接使用（Sync 语义） =====
beat.On("user.created", handler)
beat.Emit(&beat.Event{Type: "user.created", Data: []byte("data")})
beat.Off(id)

// ===== 第零层：New() 零配置（自动检测最优实现） =====
bus, _ := beat.New()        // ≥4 核 → Async，<4 核 → Sync

// ===== 第一层：ForXxx() 三核心（推荐） =====
bus, _ := beat.ForSync()    // 同步直调，~10.5ns/op
bus, _ := beat.ForAsync()   // Per-P SPSC，~26ns/op 高并发
bus, _ := beat.ForFlow()    // Pipeline 流处理，~48ns/op，批处理窗口

// ===== 第二层：Scenario() 字符串配置 =====
bus, _ := beat.Scenario("sync")
bus, _ := beat.Scenario("async")
bus, _ := beat.Scenario("flow")

// ===== 第三层：Option() 完全控制 =====
bus, _ := beat.Option(&beat.Profile{
    Name:  "async",
    Conc:  10000,
    TPS:   50000,
    Cores: 12,
})
```

## 核心 API

### 事件订阅与发布

```go
// 精确匹配
id := bus.On("user.created", func(e *beat.Event) error {
    return nil
})

// 单层通配符
bus.On("user.*", handler)    // 匹配 user.created, user.updated

// 多层通配符
bus.On("user.**", handler)   // 匹配 user.created, user.profile.updated

// 发布事件
bus.Emit(&beat.Event{Type: "user.created", Data: []byte("data")})

// 发布事件（带模式匹配）
bus.EmitMatch(&beat.Event{Type: "user.created"})

// 批量发布
events := []*beat.Event{
    {Type: "user.created", Data: []byte("alice")},
    {Type: "user.created", Data: []byte("bob")},
}
bus.EmitBatch(events)

// 取消订阅
bus.Off(id)
```

### 生命周期管理

```go
// Sync: 无需 Close（零依赖，无后台 goroutine）
bus, _ := beat.ForSync()

// Async/Flow: 必须 Close（停止 worker goroutine）
bus, _ := beat.ForAsync()
defer bus.Close()

// 优雅关闭（等待队列清空或超时）
bus.GracefulClose(5 * time.Second)
```

## 性能基准

测试环境：Intel Xeon E5-1650 v2 @ 3.50GHz (6C/12T)

### 单线程性能

```
BenchmarkAllImpls_SingleProducer/Sync     10.42 ns/op    95.97 M/s    0 B/op    0 allocs/op
BenchmarkAllImpls_SingleProducer/Async    37.41 ns/op    26.73 M/s    0 B/op    0 allocs/op
BenchmarkAllImpls_SingleProducer/Flow     47.78 ns/op    20.94 M/s    0 B/op    0 allocs/op
```

### 高并发性能 (RunParallel, 1 handler)

```
BenchmarkImplAsyncHighConcurrency/Async   26.48 ns/op    37.77 M/s    0 B/op    0 allocs/op
BenchmarkImplSyncHighConcurrency/Sync     38.37 ns/op    26.06 M/s    0 B/op    0 allocs/op
```

**关键指标**：
- **Sync**: 单线程最快 (10.5ns)，高并发 CoW 无锁读亦表现优异 (~38ns)
- **Async**: 高并发最快 (26ns)，零 CAS，Per-P SPSC ring
- **零分配**: 全部三预设热路径 0 allocs/op
- **可扩展**: Async 并发性能随核心数线性扩展

## 架构设计

### 三预设实现

| 实现 | 核心技术 | 适用场景 |
|------|---------|---------|
| **Sync** | 同步直调 + CoW atomic.Pointer | RPC 中间件、权限验证、同步钩子 |
| **Async** | Per-P SPSC ring + RCU | 事件总线、日志聚合、实时推送 |
| **Flow** | Pipeline + flowSnapshot + 批处理窗口 | 实时 ETL、窗口聚合、批量加载 |

### Async 架构细节

- **Per-P SPSC Ring**: GOMAXPROCS 个 ring（向上取整为 2 的幂），procPin 保证单写者
- **Worker 亲和性**: worker[i] 静态拥有 rings {i, i+w, i+2w, ...}
- **零 CAS**: atomic Load/Store only (x86 ≈ MOV)
- **Cached Head/Tail**: producer/consumer 本地缓存对方进度，消除常态跨核读
- **RCU 订阅**: atomic.Pointer 读无锁，写时 CoW
- **扁平化 dispatch**: 预计算 `[]core.Handler`，消除间接访问
- **SingleKey 快速路径**: 单事件类型跳过 map lookup (~16ns)
- **三级自适应空转**: PAUSE spin → Gosched → channel park

### 目录结构

```
beat/
├── core/                     # 核心接口（零依赖）
│   ├── interfaces.go        # Bus / Event / Handler 接口
│   └── matcher.go           # TrieMatcher 通配符匹配
├── internal/
│   ├── impl/                # 实现层（三预设）
│   │   ├── sync/           # Sync: 同步直调 + CoW
│   │   ├── async/          # Async: Per-P SPSC + RCU
│   │   └── flow/           # Flow: Pipeline 批处理
│   └── support/            # 支撑层
│       ├── pool/           # Event 对象池 + Arena
│       └── spsc/           # SPSC ring 无等待队列
├── optimize/               # Profile → Advisor → Factory
│   ├── profile.go         # 场景配置
│   ├── advisor.go         # 推荐引擎
│   └── factory.go         # Builder
├── util/                   # PerCPUCounter 等工具
├── test/                   # 压力测试（独立包）
└── api.go                 # 统一 API 入口
```

## 高级用法

### Async 自定义参数

```go
bus, _ := beat.Option(&beat.Profile{
    Name: "async",
    // 自动设置：Workers = NumCPU/2, RingSize = 8192
})
```

### 性能监控

```go
stats := bus.Stats()
fmt.Printf("Emitted: %d, Processed: %d, Panics: %d\n",
    stats.EventsEmitted, stats.EventsProcessed, stats.Panics)
```

### Event 对象池（可选）

```go
p := pool.Global()
evt := p.Acquire()
evt.Type = "user.created"
evt.Data = []byte("data")

bus.Emit(evt)
p.Release(evt)
```

## 使用示例

### 快速上手（包级 API）

```go
// 无需创建 Bus，无需 Close，最简用法
beat.On("order.created", func(e *beat.Event) error {
    fmt.Printf("Order: %s\n", string(e.Data))
    return nil
})

beat.Emit(&beat.Event{
    Type: "order.created",
    Data: []byte(`{"orderId":"12345"}`),
})
```

### RPC 中间件（Sync）

```go
bus, _ := beat.ForSync()

bus.On("rpc.call", authMiddleware)
bus.On("rpc.call", loggingMiddleware)
bus.On("rpc.call", metricsMiddleware)

err := bus.Emit(&beat.Event{
    Type: "rpc.call",
    Data: []byte(`{"method":"GetUser","id":123}`),
})
if err != nil {
    log.Printf("RPC failed: %v", err)
}
```

### 事件总线（Async）

```go
bus, _ := beat.ForAsync()
defer bus.Close()

bus.On("order.created", notifyUser)
bus.On("order.created", updateInventory)
bus.On("order.created", sendEmail)

bus.Emit(&beat.Event{
    Type: "order.created",
    Data: []byte(`{"orderId":"12345","amount":99.99}`),
})
```

### 日志聚合（Async）

```go
bus, _ := beat.ForAsync()
defer bus.Close()

bus.On("log.**", func(e *beat.Event) error {
    logFile.Write(e.Data)
    return nil
})

bus.Emit(&beat.Event{Type: "log.info", Data: []byte("Server started")})
bus.Emit(&beat.Event{Type: "log.error", Data: []byte("DB connection failed")})
```

### ETL 流处理（Flow）

```go
bus, _ := beat.ForFlow()
defer bus.Close()

bus.On("data.raw", func(e *beat.Event) error {
    processed := transform(e.Data)
    db.BatchInsert(processed)
    return nil
})

for record := range dataStream {
    bus.Emit(&beat.Event{Type: "data.raw", Data: record})
}
```

## 最佳实践

1. **预设选择**
   - 需要 error 返回 → **Sync**
   - 高并发 fire-and-forget → **Async**
   - 批量数据处理 → **Flow**

2. **资源管理**
   - Sync: 无需 Close，零依赖
   - Async/Flow: 必须 `defer bus.Close()`

3. **通配符使用**
   - 精确匹配优先（性能最佳）
   - `*` 适用于单层匹配
   - `**` 适用于多层匹配，避免过度匹配

4. **批量操作**
   - 高频场景使用 `EmitBatch()`
   - Flow 自动聚合批次

5. **错误处理**
   - Sync: handler 返回 error，`Emit()` 直接传递
   - Async/Flow: handler panic 自动恢复，通过 `Stats().Panics` 监控

## 性能调优

### 避免性能陷阱

- ❌ 在 handler 中阻塞 I/O（会阻塞 Async worker）
- ❌ 过度使用通配符（每次 Emit 都需要匹配）
- ❌ 在热路径上分配大对象（使用对象池）
- ✅ handler 保持轻量，异步任务推送到队列
- ✅ 批量场景使用 `EmitBatch()`
- ✅ 单事件类型场景受益于 SingleKey 优化

## 开发与测试

### 测试套件

| 文件 | 类型 | 说明 |
|------|------|------|
| `scenario_test.go` | 功能 | 场景级集成测试（ForSync/ForAsync/ForFlow/Scenario/API 层） |
| `feature_error_test.go` | 功能 | 错误处理（单/多 handler 错误、无效 Off、批量错误） |
| `feature_concurrent_test.go` | 功能 | 并发安全（On/Off/Emit 竞态、嵌套订阅、并发 Close） |
| `edge_cases_test.go` | 功能 | 边界条件（零 handler、大数据、特殊字符、慢 handler） |
| `impl_bench_test.go` | 基准 | 核心性能回归（`BenchmarkAllImpls`、Arena、EventPool） |
| `scenario_bench_test.go` | 基准 | 场景级性能（公共 API 各场景单线程+并发） |
| `feature_panic_bench_test.go` | 基准 | Panic 恢复与 Stats 收集开销 |
| `util/util_bench_test.go` | 基准 | PerCPUCounter 组件性能 |
| `internal/impl/flow/bus_test.go` | 单元 | Flow 白盒测试（批大小、超时、多阶段、并发） |
| `test/stress_test.go` | 压力 | 极端条件验证（1000 goroutine、10s 长运行，`-short` 守卫） |

### 快速验证

```bash
go fmt ./...                 # 格式检查
go vet ./...                 # 静态分析
go build ./...               # 编译
go test ./... -count=1       # 功能测试
go test -race ./... -short   # 竞态检测
```

### 性能基准

```bash
# 关键回归指标
go test -bench="BenchmarkAllImpls" -benchtime=1s -count=1 -run=^$

# 单实现详细基准
go test -bench="BenchmarkImplSync$" -benchtime=3s -count=3 -run=^$
go test -bench="BenchmarkImplAsync$" -benchtime=3s -count=3 -run=^$

# CPU / 内存分析
go test -bench="BenchmarkImplFlow$" -benchtime=3s -cpuprofile=cpu.prof
go tool pprof -top cpu.prof
```

### 性能要求

合并变更前必须验证 `-race` 通过且 benchmark 无退化（>10%）：

测试环境：Intel Xeon E5-1650 v2 @ 3.50GHz (6C/12T)

| 预设 | 单线程 | 高并发 | 分配 |
|------|--------|--------|------|
| **Sync** | ≤12 ns/op | ~38 ns/op | 0 allocs/op |
| **Async** | ~37 ns/op | ≤28 ns/op | 0 allocs/op |
| **Flow** | ~48 ns/op | — | 0 allocs/op |

## 许可证

MIT License
