# Beat

[![Go Reference](https://pkg.go.dev/badge/github.com/uniyakcom/beat.svg)](https://pkg.go.dev/github.com/uniyakcom/beat)
[![Go Report Card](https://goreportcard.com/badge/github.com/uniyakcom/beat)](https://goreportcard.com/report/github.com/uniyakcom/beat)

**高性能 Go 事件总线** — 三预设架构，零分配，零 CAS，零外部依赖
**高性能 Go 事件总线** + **消息框架** — 零外部依赖

## 特性

### 事件总线

- **零外部依赖**: 纯标准库，`go.mod` 无任何第三方 require
- **三预设架构**: Sync（同步直调）/ Async（Per-P SPSC）/ Flow（Pipeline 流处理）
- **零分配 Emit**: 全部三预设 0 B/op, 0 allocs/op
- **极致性能**: Sync ~10 ns 单线程（96M ops/s），Async ~27 ns 高并发（37M ops/s）
- **零 CAS 热路径**: Per-P SPSC ring，atomic Load/Store only（x86 ≈ 普通 MOV）
- **模式匹配**: 通配符 `*`（单层）和 `**`（多层）
- **`[256]bool` 查表**: 通配符检测零分支

### 消息框架

- **Publisher/Subscriber 接口**: Context 感知的发布/订阅契约
- **Router 路由器**: 消息管道调度中心，三种处理模式
  - **顺序处理**：单 goroutine 保证有序（默认）
  - **并发处理**：`Workers(n)` 多 worker 并行，信号量背压
  - **批量处理**：`HandleBatch` / `Batch` 批量累积触发
- **动态路由**: `Route` 按消息内容路由到不同 topic
- **死信队列**: `DLQ` 处理失败消息自动转发
- **路由级重试**: `Retry` 路由器层面兆底重试
- **流控背压**: `InFlight` 限制在途消息数
- **中间件链**: 洋葱模型，全局 + Handler 级别叠加
- **JSON 序列化**: 内置 Codec 接口 + JSON 实现
- **本地/远程统一**: `pubsub/local` 桥接 beat 引擎，适配器扩展到 Redis / Kafka / NATS / HTTP / SQL

---

## 性能

### 事件总线基准对比测试

```bash
cd _benchmarks && go test -bench="." -benchmem -benchtime=3s -count=3 -run="^$" ./...
```

#### Windows 11 — Intel Xeon E5-1650 v2 @ 3.50GHz (6C/12T)

| 场景 | beat (Sync) | beat (Async) | EventBus | ×倍 | gookit/event | ×倍 |
|------|:---:|:---:|:---:|:---:|:---:|:---:|
| **单 handler** | **11 ns** 0 alloc | 38 ns 0 alloc | 190 ns 0 alloc | **17×** | 609 ns 2 alloc | **55×** |
| **10 handler** | **26 ns** 0 alloc | 34 ns 0 alloc | 1663 ns 1 alloc | **64×** | 717 ns 2 alloc | **28×** |
| **高并发** | 28 ns 0 alloc | **27 ns** 0 alloc | 261 ns 0 alloc | **10×** | 201 ns 2 alloc | **7×** |
数据来源 [benchmarks_windows_6c12t.txt](benchmarks_windows_6c12t.txt)。 **Linux/BSD 多核性能更好**
---

## 快速开始

### 安装

```bash
go get github.com/uniyakcom/beat
```

### 事件总线（包级 API — 最简方式）

```go
import (
    "fmt"
    "github.com/uniyakcom/beat"
)

func main() {
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

### 事件总线（实例化 — 三预设任选）

```go
bus, _ := beat.ForAsync()   // Per-P SPSC 高并发
defer bus.Close()

bus.On("order.**", func(e *beat.Event) error {
    fmt.Printf("Order event: %s\n", e.Type)
    return nil
})

bus.Emit(&beat.Event{Type: "order.created", Data: []byte(`{"id":123}`)})
```

### 消息框架（Router 路由管道）

```go
import (
    "context"
    "github.com/uniyakcom/beat"
    "github.com/uniyakcom/beat/message"
    "github.com/uniyakcom/beat/pubsub/local"
    "github.com/uniyakcom/beat/router"
)

func main() {
    bus, _ := beat.ForSync()
    defer bus.Close()

    pub := local.NewPublisher(bus)
    sub := local.NewSubscriber(bus)

    r := router.NewRouter()

    // 纯消费
    r.On("notify", "order.created", sub, func(msg *message.Message) error {
        fmt.Printf("订单: %s\n", string(msg.Payload))
        return nil
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go r.Run(ctx)
    <-r.Running()

    pub.Publish(context.Background(), "order.created",
        message.New("", []byte(`{"id":123}`)))
}
```

---

## 三预设选择

| 预设 | 适用场景 | 单线程延迟 | 高并发吞吐 | error 返回 | 生命周期 |
|------|----------|-----------|-----------|-----------|---------|
| **Sync** | RPC 钩子、权限校验 | **10 ns** | ~36 ns | ✅ | 无需 Close |
| **Async** | 事件总线、日志聚合 | 41 ns | **27 ns** | ❌ | 需 Close |
| **Flow** | ETL 流处理、批量加载 | 47 ns | — | ❌ | 需 Close |

```go
// 包级 API（Sync 语义）
beat.On("event", handler)
beat.Emit(event)

// 三核心
bus, _ := beat.ForSync()     // 同步直调
bus, _ := beat.ForAsync()    // Per-P SPSC
bus, _ := beat.ForFlow()     // Pipeline

// 自动检测
bus, _ := beat.New()         // ≥4 核 → Async，<4 核 → Sync

// 字符串配置
bus, _ := beat.Scenario("async")

// 完全控制
bus, _ := beat.Option(&beat.Profile{Name: "async", Conc: 10000, TPS: 50000})
```

---

## 消息框架

### 适配器生态

**适配器均是简单示例，尚未优化，请勿用于生产环境。**

| 模块 | 传输层 | 特点 |
|------|--------|------|
| `beat-redis` | Redis Pub/Sub + Streams | Pipeline 批量、DirectFields 零封装、Consumer Group |
| `beat-kafka` | Apache Kafka (sarama) | Partition Key、Header 传播、SyncProducer |
| `beat-nats` | NATS Core + JetStream | 持久化、消费者组、Header 传播 |
| `beat-http` | HTTP Webhook | 零依赖 net/http、Subscriber 内置服务端 |
| `beat-sql` | SQL Outbox | 事务原子写入（PublishInTx）、多数据库支持 |


### 核心类型

#### Message

```go
type Message struct {
    UUID      string            // 消息唯一标识（Per-P 批量 UUID v4）
    Key       string            // 分区/路由键（Kafka partition、Redis key）
    Metadata  Metadata          // 元数据 map[string]string
    Payload   []byte            // 消息体
    Timestamp time.Time         // 创建时间戳（自动设置）
    // 内部: state atomic.Uint32 — CAS 状态机管理 Ack/Nack（替代 2×sync.Once）
}

// 构造
msg := message.New("", payload)           // 通用（含 Ack/Nack channel）
msg := message.NewPub("", payload)        // 发布专用（无 Ack，省分配）
msg.Key = "user-123"                              // 设置路由键
```

#### Publisher / Subscriber

```go
// Publisher 发布者接口
type Publisher interface {
    Publish(ctx context.Context, topic string, messages ...*Message) error
    Close() error
}

// Subscriber 订阅者接口
type Subscriber interface {
    Subscribe(ctx context.Context, topic string) (<-chan *Message, error)
    Close() error
}
```

### Router 路由器

Router 是消息框架的调度中心：Subscriber → 中间件链 → Handler → Publisher。

#### 基本用法

```go
r := router.NewRouter()

// 纯消费（不产出）
r.On("notify", "order.created", sub, func(msg *message.Message) error {
    sendEmail(msg.Payload)
    return nil
})

// 完整管道：subscribe → process → publish
r.Handle(
    "transform",
    "raw.data", inSub,
    "processed.data", outPub,
    func(msg *message.Message) ([]*message.Message, error) {
        result := transform(msg.Payload)
        return []*message.Message{message.New("", result)}, nil
    },
)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
r.Run(ctx)
```

#### 并发处理

```go
// 8 个 worker 并行消费，信号量自动背压
r.Handle("fast", "events", sub, "out", pub, handler).
    Workers(8)
```

#### 批量处理

```go
// 累积 100 条或 1s 超时后触发批量处理
r.HandleBatch(
    "bulk-insert",
    "log.entries", sub,
    "", nil, // 无产出
    func(msgs []*message.Message) ([]*message.Message, error) {
        rows := make([]Row, len(msgs))
        for i, m := range msgs {
            rows[i] = parseRow(m.Payload)
        }
        return nil, db.BulkInsert(rows)
    },
    100,           // batchSize
    time.Second,   // batchTimeout
)

// 便捷写法
r.OnBatch("bulk", "log.entries", sub, func(msgs []*message.Message) error {
    return db.BulkInsert(toRows(msgs))
}, 100, time.Second)
```

#### 动态路由

```go
// 根据消息内容路由到不同 topic
r.Handle("route", "events", sub, "default.out", pub, handler).
    Route(func(msg *message.Message) string {
        switch msg.Metadata.Get("type") {
        case "urgent":
            return "priority.queue"
        case "bulk":
            return "batch.queue"
        default:
            return "" // 使用默认 publishTopic
        }
    })
```

#### 死信队列 (DLQ)

```go
dlqPub, _ := beatredis.NewPublisher(redisCfg)

r.Handle("process", "orders", sub, "done", pub, handler).
    Retry(3).
    DLQ(router.DLQConfig{
        Topic:     "orders.dlq",
        Publisher: dlqPub,
    })
// 处理失败 3 次后消息自动发送到 orders.dlq，附带 dlq_reason / dlq_handler / dlq_topic 元数据
```

#### 流控

```go
// 限制最大在途消息数（与并发配合使用）
r.Handle("controlled", "events", sub, "out", pub, handler).
    Workers(16).
    InFlight(8) // 最多 8 条未确认消息
```

### 中间件

```go
import (
    "github.com/uniyakcom/beat/middleware/retry"
    "github.com/uniyakcom/beat/middleware/timeout"
    "github.com/uniyakcom/beat/middleware/recoverer"
    "github.com/uniyakcom/beat/middleware/logging"
    "github.com/uniyakcom/beat/middleware/correlation"
)

r := router.NewRouter()

// 全局中间件（洋葱模型：外层先执行）
r.Use(
    recoverer.New(),                          // panic → error 恢复
    correlation.New(),                        // correlation_id 传播（FastUUID）
    logging.New(slog.Default()),              // slog 处理日志
    timeout.New(5 * time.Second),             // 消息处理超时
    retry.New(retry.Config{MaxRetries: 3}),   // 指数退避重试
)

// Abandoned Context 模式（超时后 handler 可继续完成 DB 事务等清理）
r.Use(timeout.NewWithConfig(timeout.Config{
    Timeout:      5 * time.Second,
    AllowOverrun: true,   // 超时仅切断 Context，不中断 goroutine
}))

// Handler 专属中间件（在全局之后执行）
handler.AddMiddleware(customMiddleware)
```

> **注意**: 中间件仅对单条处理器（HandlerFunc）生效。批量处理器（BatchFunc）不经过中间件链。

### 序列化

```go
import "github.com/uniyakcom/beat/marshal"

m := marshal.JSON{}
data, _ := m.Marshal("topic", msg)          // Message → []byte
restored, _ := m.Unmarshal("topic", data)   // []byte → Message
```

---

## 适配器

所有适配器共享相同的 `message.Publisher` / `message.Subscriber` 接口，通过 Router 统一调度。

### Redis

```go
import beatredis "github.com/uniyakcom/beat-redis"

pub, _ := beatredis.NewPublisher(beatredis.PublisherConfig{
    Client:         rdb,
    UseStreams:      true,
    DirectFields:   true,       // 零封装直存字段
    EnablePipeline: true,       // 批量 Pipeline
})

sub, _ := beatredis.NewSubscriber(beatredis.SubscriberConfig{
    Client:       rdb,
    ConsumerGroup: "my-service",
})
```

### Kafka

```go
import beatkafka "github.com/uniyakcom/beat-kafka"

pub, _ := beatkafka.NewPublisher(beatkafka.PublisherConfig{
    Brokers: []string{"localhost:9092"},
})
// msg.Key 自动映射为 Kafka partition key
// msg.Metadata 传播为 Kafka headers
```

### NATS

```go
import beatnats "github.com/uniyakcom/beat-nats"

pub, _ := beatnats.NewPublisher(beatnats.PublisherConfig{
    Conn:      nc,
    JetStream: js, // 可选：启用持久化
})
```

### HTTP

```go
import beathttp "github.com/uniyakcom/beat-http"

pub, _ := beathttp.NewPublisher(beathttp.PublisherConfig{
    EndpointURL: "https://api.example.com/webhooks",
})
// POST {url}/{topic}，Header 传播 metadata
```

### SQL (Outbox)

```go
import beatsql "github.com/uniyakcom/beat-sql"

pub, _ := beatsql.NewPublisher(beatsql.PublisherConfig{DB: db})

// 事务原子写入
tx, _ := db.Begin()
tx.Exec("INSERT INTO orders ...")
pub.PublishInTx(tx, "order.created", msg)
tx.Commit()
```

---

## 架构设计

### 事件总线

| 实现 | 核心技术 | 适用场景 |
|------|---------|---------|
| **Sync** | 同步直调 + CoW atomic.Pointer | RPC 中间件、权限验证 |
| **Async** | Per-P SPSC ring + RCU | 事件总线、日志聚合 |
| **Flow** | Pipeline + 批处理窗口 | ETL、窗口聚合 |

**Async 架构要点**:
- Per-P SPSC Ring：procPin 保证单写者，零 CAS
- Worker 亲和性：worker[i] 静态拥有 rings {i, i+w, i+2w, ...}
- 三级自适应空转：PAUSE spin → Gosched → channel park

### 消息框架

```
Subscriber ──────── Router ──────── Publisher
     │                 │                 │
  Subscribe(ctx)   中间件链(洋葱)    Publish(ctx)
     │                 │                 │
  <-chan *Message  HandlerFunc     动态路由(topicFunc)
                       │
                ┌──────┼──────┐
              顺序    并发    批量
          (1 worker) (N sem) (accumulate)
                       │
                   DLQ (失败)
```

### Handler 配置

```go
handler := r.Handle(name, subTopic, sub, pubTopic, pub, fn)

handler.
    Workers(8).              // 8 worker 并行
    InFlight(4).             // 最多 4 条在途
    Route(routeFn).          // 动态路由
    Retry(3).                // 路由级重试
    DLQ(dlqCfg).             // 死信队列
    AddMiddleware(myMiddleware) // Handler 专属中间件
```

### 目录结构

```
beat/
├── core/                     # 核心接口（Bus / Event / Handler）
│   ├── interfaces.go
│   └── matcher.go           # TrieMatcher 通配符匹配
├── message/                  # 消息框架核心类型
│   ├── message.go           # Message（UUID / Key / Timestamp / Ack / Nack）
│   ├── metadata.go          # map[string]string 元数据
│   ├── publisher.go         # Publisher 接口（ctx 感知）
│   ├── subscriber.go        # Subscriber 接口
│   └── uuid.go              # UUID v4（Per-P 批量生成）+ FastUUID（零互斥）
├── router/                   # 消息路由器
│   ├── router.go            # 调度中心（顺序/并发/批量循环、DLQ、动态路由）
│   ├── handler.go           # Handler 配置（并发/批量/DLQ/流控）
│   ├── middleware.go        # Middleware 类型
│   └── plugin.go            # Plugin 生命周期钩子
├── pubsub/local/            # beat Bus ↔ Publisher/Subscriber 桥接
├── middleware/               # 内置中间件
│   ├── retry/               # 指数退避重试
│   ├── timeout/             # 消息处理超时
│   ├── recoverer/           # panic → error 恢复
│   ├── logging/             # slog 日志
│   └── correlation/         # correlation_id 传播
├── marshal/                  # 序列化（Codec 接口 + JSON）
├── optimize/                 # Profile → Advisor → Factory
├── internal/impl/           # 三预设实现（sync / async / flow）
├── internal/support/        # SPSC ring、对象池、可切换锁等基础设施
│   ├── noop/                # 可切换锁（nil mutex = 零开销）
│   ├── pool/                # 事件对象池 + Arena 内存管理
│   ├── spsc/                # Per-P SPSC ring buffer
│   └── wpool/               # Worker pool（done channel 安全关闭）
├── util/                    # PerCPUCounter 等工具
└── api.go                   # 统一 API 入口
```

---

## 性能优化技术

### P0 — 热路径零分配

| 技术 | 文件 | 说明 |
|------|------|------|
| **Per-P UUID 批量生成** | `message/uuid.go` | `NewUUID()` 使用 Per-P `cryptoBuf[64]`，一次 `crypto/rand.Read(512B)` 生成 32 个 UUID；`FastUUID()` 使用 Per-P `math/rand` + `go:linkname procPin/procUnpin`，热路径零互斥 |
| **CAS 状态机** | `message/message.go` | `atomic.Uint32` 状态字段（4 字节）替代 `2×sync.Once`（~24 字节），Ack/Nack 单原子操作 |
| **可切换锁 (nil mutex)** | `internal/support/noop/` | `noop.Mutex` / `noop.RWMutex`：nil 指针 = 零开销空操作，对象池竞态保护可按需开关 |
| **固定数组** | `core/matcher.go` | `var pathBuf [maxTrieDepth+1]*node` 栈分配替代 `make([]*node)`，Trie Remove 零堆分配 |
| **`[256]bool` 查表** | `core/matcher.go` | `wildcardChars[s[i]]` 替代 `s[i] == '*'`，零分支通配符检测 |

### P1 — 资源保护与抗压

| 技术 | 文件 | 说明 |
|------|------|------|
| **Arena 内存池保护** | `internal/support/pool/pool.go` | `maxChunks` 限制总 chunk 数（默认 256 = 16MB）；超大分配（> chunkSize/2）旁路至 `make`；超限时降级为普通分配 |
| **Worker Pool 安全关闭** | `internal/support/wpool/wpool.go` | `done chan struct{}` + `select` 模式替代 `close(channel)`，Submit/Release 全路径无 panic |
| **快/慢路径分离** | `internal/impl/flow/bus.go` | `emitSlow` 提取为 `//go:noinline` 独立函数，正常路径的指令缓存命中率不受异常分支污染 |

### P2 — 中间件增强

| 技术 | 文件 | 说明 |
|------|------|------|
| **Abandoned Context** | `middleware/timeout/timeout.go` | `Config{AllowOverrun: true}` 模式：超时后 handler goroutine 继续执行，使用仅保留 `Value()` 的 `detachedContext`（Done/Err/Deadline 返回零值），避免 DB 事务半提交 |
| **FastUUID 相关性 ID** | `middleware/correlation/` | 相关性中间件使用 `message.FastUUID()` 替代 `NewUUID()`，批量事件流中减少 `crypto/rand` 系统调用 |

---

## 安全设计

经过完整的代码安全审计，以下问题已识别并修复：

| 等级 | 问题 | 修复方案 |
|------|------|---------|
| **Critical** | `detachedContext` 泄漏父 Context 的取消信号 | `Done()→nil`、`Err()→nil`、`Deadline()→(zero,false)`，仅保留 `Value()` |
| **Critical** | `wpool.Submit` 向已关闭 channel 发送导致 panic | `done chan struct{}` + `select` 全路径保护，`Release()` 使用 CAS 幂等关闭 |
| **High** | `NewPub` 消息的 `Ack()/Nack()` close nil channel | CAS 前增加 `if m.ackCh == nil { return }` nil 守卫 |
| **High** | Arena chunk 从 `sync.Pool` 取出时偏移量未重置 | Put/Get 时显式重置 `offset = 0` |
| **High** | `activeChunks` 计数器只增不减导致过早降级 | 移除独立计数器，依赖 `sync.Pool` 自身生命周期管理 |
| **High** | `flow.EmitBatch` 忽略 `push()` 返回值导致事件丢失 | 复用 `Emit()` 方法（含 `emitSlow` 降级回退） |
| **Medium** | `matcher.Off` 的 Remove 调用次数与 Add 不匹配 | `for range subs { matcher.Remove(k) }` 按订阅数循环 |
| **Medium** | `correlation` 中间件产出消息可能为 nil | 增加 `if p == nil { continue }` 空指针守卫 |
| **Medium** | Timestamp 使用 `unsafe` 指针清零 | 改为安全的 `evt.Timestamp = time.Time{}` 赋值 |

---

## 最佳实践

### 事件总线

- 需要 error 返回 → **Sync**
- 高并发 fire-and-forget → **Async**
- 批量数据处理 → **Flow**
- Async/Flow 必须 `defer bus.Close()`
- handler 保持轻量，避免阻塞 I/O

### 消息框架

- **ctx 传播**: Publisher.Publish 第一个参数是 `context.Context`，Router 自动使用 `msg.Context()` 传播超时
- **Message.Key**: Kafka partition、Redis routing 等分区键，适配器自动映射
- **并发 vs 有序**: 默认顺序处理保证有序；`Workers(n)` 牺牲有序换吞吐
- **批量入库**: 高频写入用 `HandleBatch`，减少数据库往返
- **DLQ 兆底**: 生产环境务必配置 `DLQ`，避免毒消息阻塞队列
- **中间件顺序**: recoverer 放最外层（捕获所有 panic），retry 放最内层（仅重试业务逻辑）

---

## 开发与测试

### 测试套件

| 文件 | 类型 | 说明 |
|------|------|------|
| `scenario_test.go` | 功能 | ForSync/ForAsync/ForFlow/Scenario 全场景 |
| `feature_error_test.go` | 功能 | 单/多 handler 错误、批量错误 |
| `feature_concurrent_test.go` | 功能 | On/Off/Emit 竞态、嵌套订阅、并发 Close |
| `edge_cases_test.go` | 边界 | 零 handler、大数据、特殊字符 |
| `router/router_test.go` | 功能 | Router On/Handle/中间件/panic 恢复 |
| `pubsub/local/local_test.go` | 功能 | 本地 Pub/Sub 收发、Context 取消 |
| `marshal/marshal_test.go` | 功能 | JSON 序列化往返 |
| `middleware/middleware_test.go` | 功能 | 中间件组合测试 |
| `impl_bench_test.go` | 基准 | 核心性能回归（三预设、Arena、EventPool） |
| `test/stress_test.go` | 压力 | 1000 goroutine · 10s 长运行 |

### 快速验证

```bash
go build ./...              # 编译
go vet ./...                # 静态分析
go test ./... -count=1      # 功能测试
go test -race ./... -short  # 竞态检测
gofmt -s -w ./               # 格式化整个项目
```

### 性能基准

```bash
# 关键回归指标
go test -bench="BenchmarkAllImpls" -benchtime=1s -count=1 -run=^$

# Redis 对比 Watermill
cd ../beat-redis/_benchmarks
go test -bench="." -benchmem -benchtime=3s -count=3 -run="^$" ./...
```

### 基准测试脚本

自动检测 CPU 拓扑，运行完整基准测试 + 竞品对比，生成 `benchmarks_*.txt` 基线文件。

```bash
# Linux / macOS
./scripts/bench.sh              # 默认 benchtime=3s, count=1
./scripts/bench.sh 5s 3         # benchtime=5s, count=3
BENCH_SKIP_COMPARE=1 ./scripts/bench.sh  # 跳过竞品对比
```

```powershell
# Windows PowerShell
.\scripts\bench.ps1                          # 默认 benchtime=3s, count=1
.\scripts\bench.ps1 -BenchTime 5s -Count 3   # benchtime=5s, count=3
.\scripts\bench.ps1 -SkipCompare              # 跳过竞品对比
```

输出文件命名规则：`benchmarks_{os}_{cores}c{threads}t[_{vcpus}vc].txt`（cores = 单 socket 物理核心数）。

**关键指标**:

| 指标 | Windows 11 (6C/12T) | Linux (1C/2T) | Linux (2C/4T) | Linux (4C/8T) |
|------|:---:|:---:|:---:|:---:|
| Sync 单线程 | 10.4 ns/op | 9.4 ns/op | 12.4 ns/op | 21 ns/op |
| Async 高并发 | 27 ns/op | 30 ns/op | 69 ns/op | 27 ns/op |
| Flow 单线程 | 47 ns/op | 53 ns/op | 58 ns/op | 91 ns/op |
| 分配 | **0 allocs/op** | **0 allocs/op** | **0 allocs/op** | **0 allocs/op** |
| 测试数据 | [benchmarks_windows_6c12t.txt](benchmarks_windows_6c12t.txt) | [benchmarks_linux_1c2t_2vc.txt](benchmarks_linux_1c2t_2vc.txt) | [benchmarks_linux_2c4t_4vc.txt](benchmarks_linux_2c4t_4vc.txt) | [benchmarks_linux_4c8t_8vc.txt](benchmarks_linux_4c8t_8vc.txt) |
---

## 许可证

MIT License
