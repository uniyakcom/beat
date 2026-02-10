# 竞品基准对比

独立模块，不污染主项目 `go.mod`。

## 被测库

| 库 | 星标 | 分发机制 | 入选理由 |
|---|------|---------|---------|
| **beat** (Sync/Async) | — | 同步直调 / Per-P SPSC | 本项目 |
| **asaskevich/EventBus** | 2k⭐ | reflect.Value.Call + Mutex | 最流行 Go 事件总线 |
| **gookit/event** | 565⭐ | 接口分发 + 优先级 + 通配符 | 功能丰富的同类产品 |

### 排除的库

| 库 | 原因 |
|---|------|
| olebedev/emitter | Channel-based pubsub，每次 Emit 创建 goroutine，范式完全不同 |
| reactivex/rxgo | Reactive Observable 流，非事件总线 |
| deatil/go-events | WordPress Action/Filter 模式，8⭐，范式不同 |

## 测试场景

| 场景 | 说明 |
|------|------|
| `Emit_1Handler` | 单 handler 精确匹配（核心热路径） |
| `Emit_10Handlers` | 10 个 handler 精确匹配（多 handler 扇出） |
| `Parallel_Emit` | 高并发 RunParallel（并发吞吐） |

## 运行

```bash
cd _benchmarks
go mod tidy
go test -bench=. -benchmem -benchtime=3s -count=3 -run=^$
```

保存结果：

```bash
go test -bench=. -benchmem -benchtime=3s -count=3 -run=^$ | tee results.txt
```
