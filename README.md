# Beat

[![Go Reference](https://pkg.go.dev/badge/github.com/uniyakcom/beat.svg)](https://pkg.go.dev/github.com/uniyakcom/beat)
[![Go Report Card](https://goreportcard.com/badge/github.com/uniyakcom/beat)](https://goreportcard.com/report/github.com/uniyakcom/beat)

**高性能 Go 事件总线** — 3 种实现 + 2 种发布模式 + 4 层 API，零分配，零 CAS，零外部依赖
**高性能 Go 事件总线** + **消息框架** — 零外部依赖

## 特性

### 事件总线

- **零外部依赖**: 纯标准库，`go.mod` 无任何第三方 require
- **3 种 Bus 实现**: Sync（同步直调）/ Async（Per-P SPSC）/ Flow（Pipeline 流处理）
- **2 种发布模式**: `Emit`（安全路径，defer/recover 保护）/ `UnsafeEmit`（零保护，极致性能）
- **4 层 API**: 零配置 `New()` / 场景 `ForXxx()` / 字符串 `Scenario()` / 完全控制 `Option()`
- **零分配 Emit**: 全部三实现 0 B/op, 0 allocs/op
- **极致性能**: UnsafeEmit ~4.4 ns（243M ops/s），Sync Emit ~15 ns，Async ~33 ns（31M ops/s）
- **零 CAS 热路径**: Per-P SPSC ring，atomic Load/Store only（x86 ≈ 普通 MOV）
- **SPSC 共享调度器**: Sync 异步模式与 Async 共用同一 SPSC 分片架构
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

#### Linux — Intel Xeon Platinum @ 2.50GHz (1C/2T, 2 vCPU)

**同步对比**

| 场景 | beat Unsafe | beat Sync | EventBus | gookit/event |
|------|:---:|:---:|:---:|:---:|
| **单 handler** | **4.4 ns** 0 alloc | 14 ns 0 alloc | 142 ns 0 alloc | 414 ns 2 alloc |
| **10 handler** | **18 ns** 0 alloc | 25 ns 0 alloc | 1203 ns 1 alloc | 469 ns 2 alloc |
| **高并发** | **5.7 ns** 0 alloc | 17 ns 0 alloc | 171 ns 0 alloc | 457 ns 2 alloc |

**异步对比**

| 场景 | beat Async | EventBus Async | gookit Async |
|------|:---:|:---:|:---:|
| **单 handler** | **33 ns** 0 alloc | 1287 ns 1 alloc | 655 ns 4 allocs |
| **高并发** | **31 ns** 0 alloc | 1258 ns 1 alloc | 1010 ns 5 allocs |

- UnsafeEmit 高并发 ~5.7 ns/op — 纯 atomic.Pointer Load + handler 调用，零保护
- Sync Emit ~15 ns/op — 含 defer/recover + PerCPU 计数
- Async 高并发 ~31 ns/op — Per-P SPSC ring，零 CAS
- Async vs 竞品异步: 20×～41× 快，零分配

数据来源 [benchmarks_linux_1c2t_2vc.txt](benchmarks_linux_1c2t_2vc.txt)。 多核环境数据见 [benchmarks_linux_4c8t_8vc.txt](benchmarks_linux_4c8t_8vc.txt)，Windows 对比数据见 [benchmarks_windows_6c12t.txt](benchmarks_windows_6c12t.txt)
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

## 3 种 Bus 实现 + 2 种发布模式 + 4 层 API

### 3 种 Bus 实现

| 实现 | 核心技术 | 适用场景 | Emit | UnsafeEmit | 高并发 | error 返回 | 生命周期 |
|------|---------|---------|:---:|:---:|:---:|:---:|:---:|
| **Sync** | 同步直调 + CoW atomic.Pointer | RPC 钩子、权限校验、API 中间件 | **15 ns** | **4.4 ns** | 175 ns | ✅ | 无需 Close |
| **Async** | Per-P SPSC ring + RCU + 三级空转 | 事件总线、日志聚合、实时推送、高频交易 | 33 ns | = Emit | **32 ns** | ❌ | 需 Close |
| **Flow** | MPSC ring + Stage Pipeline + per-shard 精准唤醒 | ETL 流处理、窗口聚合、批量数据加载 | 70 ns | — | 455 ns | ❌ | 需 Close |

### 2 种发布模式

| 模式 | Sync | Async | Flow | 说明 |
|------|:---:|:---:|:---:|------|
| **`Emit`** | defer/recover + PerCPU 计数 | SPSC Enqueue + worker dispatch | ring push + batch | 安全路径，捕获 handler panic，更新 Stats |
| **`UnsafeEmit`** | 纯 CoW 快照查找 + handler 调用 | = Emit | — | 零保护，panic 传播到调用方，不计数 |

> Sync 实现还支持 **SyncAsync 子模式**（`NewAsync()`）：复用 Async 的 SPSC 分片调度器异步执行 handler，额外提供 `ErrorReporter` 接口（`LastError()` / `ClearError()`），适合需要错误追踪 + 异步的场景。

### 4 层 API

| 层级 | API | 说明 |
|------|-----|------|
| **L0 零配置** | `beat.New()` | 自动检测：≥4 核用 Async，<4 核用 Sync |
| **L1 场景** | `beat.ForSync()` / `ForAsync()` / `ForFlow()` | 选定实现 |
| **L2 字符串** | `beat.Scenario("async")` | 配置文件/环境变量驱动 |
| **L3 完全控制** | `beat.Option(profile)` | 自定义 Profile（并发数、TPS、Arena、批处理超时等） |
| **包级** | `beat.On()` / `beat.Emit()` | 全局 Sync 单例，零初始化 |

```go
// 包级 API（Sync 语义）
beat.On("event", handler)
beat.Emit(event)           // 安全路径，~15 ns
beat.UnsafeEmit(event)     // 零保护，~4.4 ns

// L1 三核心
bus, _ := beat.ForSync()     // 同步直调
bus, _ := beat.ForAsync()    // Per-P SPSC
bus, _ := beat.ForFlow()     // Pipeline

// L0 自动检测
bus, _ := beat.New()         // ≥4 核 → Async，<4 核 → Sync

// L2 字符串配置
bus, _ := beat.Scenario("async")

// L3 完全控制
bus, _ := beat.Option(&beat.Profile{Name: "async", Conc: 10000, TPS: 50000})

// 发布模式
bus.Emit(evt)            // 安全：defer/recover + Stats 计数
bus.UnsafeEmit(evt)      // 极致：零 defer、零计数，panic 直接传播
bus.EmitMatch(evt)       // 通配符匹配（安全）
bus.UnsafeEmitMatch(evt) // 通配符匹配（极致）
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

3 种 Bus 实现（Sync / Async / Flow）+ 2 种发布模式（Emit / UnsafeEmit）+ 4 层 API。

| 实现 | 核心技术 | 适用场景 |
|------|---------|----------|
| **Sync** | 同步直调 + CoW atomic.Pointer + defer/recover | RPC 中间件、权限验证 |
| **Sync (SyncAsync)** | 复用 SPSC 分片调度器异步分发 + ErrorReporter | 需要异步 + 错误追踪 |
| **Async** | Per-P SPSC ring + RCU + 三级空转 | 事件总线、日志聚合 |
| **Flow** | MPSC ring + Pipeline + per-shard 精准唤醒 + 自适应分片 | ETL、窗口聚合 |

**SPSC 共享架构**: Sync 异步模式与 Async 共用同一 `sched.ShardedScheduler`，确保 Emit 热路径一致：
- Producer: `procPin → SPSC Enqueue (~3 ns) → procUnpin → wake`
- Consumer: `SPSC Dequeue → dispatch(snap, handlers) → processed++`
- Worker: 三级自适应空转（PAUSE spin → Gosched → channel park）

**发布模式**:
- `Emit`: 安全路径，defer/recover 捕获 handler panic，PerCPU 计数器更新 Stats
- `UnsafeEmit`: 零保护路径，无 defer、无计数，~4.4 ns（Sync）/ ~5.7 ns（高并发）

**自适应优化**:
- PerCPUCounter: 最小 8 slot 保底，低核环境哈希冲突率从 ~100%→~25%
- Flow 分片: 跟随 NumCPU 自适应，下限从 4→22，低核减少不必要的 consumer 争抢
- Flow 唤醒: per-shard 独立 notifyChs[]，Emit 精准唤醒目标分片，EmitBatch 位图追踪
- New(): ≥4 核→Async，<4 核→Sync，自动选择最优实现

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
├── internal/impl/           # 三实现（sync / async / flow）
├── internal/support/        # 基础设施
│   ├── noop/                # 可切换锁（nil mutex = 零开销）
│   ├── pool/                # 事件对象池 + Arena 内存管理
│   ├── sched/               # SPSC 分片调度器（Sync 异步 + Async 共用）
│   ├── spsc/                # Per-P SPSC ring buffer
│   └── wpool/               # Worker pool（分片 channel + 安全关闭）
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
| **SPSC 共享调度器** | `internal/support/sched/` | Sync 异步模式与 Async 共用同一 `ShardedScheduler`，procPin → SPSC Enqueue (~3 ns) 替代 channel send (~200 ns) |
| **Arena chunkPool 回收** | `internal/support/pool/` | `sync.Pool` 回收 ArenaChunk（重置 offset，复用 buf），消除重复分配，0 B/op |

### P1 — 资源保护与抗压

| 技术 | 文件 | 说明 |
|------|------|------|
| **Arena 内存池** | `internal/support/pool/pool.go` | CAS bump allocator + `chunkPool sync.Pool` 回收 chunk（重置 offset，复用 buf）；超大分配（> chunkSize/2）旁路至 `make` |
| **Worker Pool 安全关闭** | `internal/support/wpool/wpool.go` | `done chan struct{}` + `select` 模式替代 `close(channel)`，Submit/Release 全路径无 panic；双层 worker/workerInner recover 模式 |
| **快/慢路径分离** | `internal/impl/flow/bus.go` | `emitSlow` 提取为 `//go:noinline` 独立函数，正常路径的指令缓存命中率不受异常分支污染 |
| **per-shard 精准唤醒** | `internal/impl/flow/bus.go` | consumer 使用 per-shard `notifyChs[]` + `ticker` 阻塞等待替代全局 notify 和 `select{default}` 忙循环；Emit 精准唤醒目标分片，EmitBatch 位图追踪仅唤醒有数据的分片，消除跨分片虚假唤醒 |
| **Emit 安全/极致双路径** | `internal/impl/sync/bus.go` | `emitSyncSafe`（noinline + defer/recover）与 `UnsafeEmit`（nosplit，零 defer）分离，让用户根据场景选择安全或性能 |
| **PerCPUCounter 自适应保底** | `util/util.go` | 最小 8 slot（低核 2-4 vCPU 哈希冲突率从 ~100% 降至 ~25%），高核（≥8）无变化 |
| **Flow 自适应分片** | `internal/impl/flow/bus.go` | 分片数下限从 4 降至 2，跟随 NumCPU 自适应；低核减少 50% 不必要的 consumer 争抢 |
| **Sync Stats 推导优化** | `internal/impl/sync/bus.go` | 同步模式 Stats() 中 Processed 推导自 Emitted（同步完成 = 已处理），消除同步热路径的冗余计数 |

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
| **High** | Arena chunk 回收机制缺失导致内存浪费 | `chunkPool sync.Pool` 回收 chunk（重置 offset，复用 buf） |
| **High** | `flow.EmitBatch` 忽略 `push()` 返回值导致事件丢失 | 复用 `Emit()` 方法（含 `emitSlow` 降级回退） |
| **Medium** | `matcher.Off` 的 Remove 调用次数与 Add 不匹配 | `for range subs { matcher.Remove(k) }` 按订阅数循环 |
| **Medium** | `correlation` 中间件产出消息可能为 nil | 增加 `if p == nil { continue }` 空指针守卫 |
| **Medium** | Timestamp 使用 `unsafe` 指针清零 | 改为安全的 `evt.Timestamp = time.Time{}` 赋值 |
| **Critical** | Flow consumer `LockOSThread` + 忙等待耗尽全部 vCPU，导致 VM 心跳丢失自动重启 | 移除 `LockOSThread`，`select{default}` 忙循环改为 per-shard `notifyChs[]` + `ticker` 信号量阻塞唤醒 |
| **Medium** | PerCPUCounter 在低核环境（2 vCPU）仅 2 slot，哈希冲突率 ~100% | 自适应保底 8 slot，高核无影响 |
| **Medium** | Flow 强制最少 4 分片，低核（2 vCPU）消费者过多争抢 CPU | 自适应分片下限从 4 降至 2，跟随 NumCPU |
| **Medium** | Flow 全局单 notify channel 导致跨分片虚假唤醒 | per-shard 独立 notifyChs[]，Emit 精准唤醒目标分片 |

---

## 最佳实践

### 事件总线

- 需要 error 返回 → **Sync** (`Emit`)
- 高并发 fire-and-forget → **Async** (`Emit`)
- 批量数据处理 → **Flow**
- 极致性能、handler 不会 panic → **Sync** (`UnsafeEmit`, ~4.4 ns 单线程, ~5.7 ns 高并发)
- 异步 + 错误追踪 → **Sync** (`NewAsync()`, ~29 ns)
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
| `impl_bench_test.go` | 基准 | 核心性能回归（三实现、Arena、EventPool） |
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
go test -bench="BenchmarkImpl" -benchtime=1s -count=1 -run=^$

# 竞品对比（同步 + 异步）
cd _benchmarks
go test -bench=. -benchmem -benchtime=3s -count=3 -run="^$" ./...
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

| 指标 | Linux (1C/2T, 2 vCPU) | Linux (4C/8T, 8 vCPU) | Windows 11 (6C/12T) |
|------|:---:|:---:|:---:|
| Sync 单线程 | **15 ns/op** | 23 ns/op | 21 ns/op |
| UnsafeEmit 单线程 | **4.4 ns/op** | 8.5 ns/op | 8.2 ns/op |
| SyncAsync 单线程 | **29 ns/op** | 49 ns/op | 51 ns/op |
| Async 单线程 | **33 ns/op** | 42 ns/op | 38 ns/op |
| Async 高并发 | 32 ns/op | 24 ns/op | 17 ns/op |
| Flow 单线程 | **70 ns/op** | 153 ns/op | 65 ns/op |
| Flow 高并发 | 455 ns/op | 100 ns/op | 99 ns/op |
| Sync 高并发 | 175 ns/op | 57 ns/op | — |
| BatchBulkInsert 吞吐 | **20.6 M/s** | 18.9 M/s | 18.6 M/s |
| 分配 | **0 allocs/op** | **0 allocs/op** | **0 allocs/op** |
| 测试数据 | [benchmarks_linux_1c2t_2vc.txt](benchmarks_linux_1c2t_2vc.txt) | [benchmarks_linux_4c8t_8vc.txt](benchmarks_linux_4c8t_8vc.txt) | [benchmarks_windows_6c12t.txt](benchmarks_windows_6c12t.txt) |
---

## 许可证

MIT License
