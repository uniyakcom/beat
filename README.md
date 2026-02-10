# Beat

[![Go Reference](https://pkg.go.dev/badge/github.com/uniyakcom/beat.svg)](https://pkg.go.dev/github.com/uniyakcom/beat)
[![Go Report Card](https://goreportcard.com/badge/github.com/uniyakcom/beat)](https://goreportcard.com/report/github.com/uniyakcom/beat)

[English](README.md) | [中文](README_zh.md)

High-performance Go event bus — three paradigms, zero allocation, zero CAS, zero dependencies.

## Features

- **Zero Dependencies**: Pure standard library, no third-party requires in `go.mod`
- **Three Paradigms**: Sync (direct call) / Async (Per-P SPSC) / Flow (pipeline)
- **Zero-Allocation Emit**: All three paradigms 0 B/op, 0 allocs/op
- **Extreme Performance**: Async 26 ns/op high concurrency (38M ops/s), Sync 10.5 ns/op single-thread (95M ops/s)
- **Zero CAS Hot Path**: Per-P SPSC ring, atomic Load/Store only (≈ plain MOV on x86)
- **Concurrency Safe**: RCU subscription management + CoW snapshots
- **Pattern Matching**: Wildcard `*` (single level) and `**` (multi-level)
- **Simple API**: `ForSync()` / `ForAsync()` / `ForFlow()`
- **Package-Level API**: `beat.On()` / `beat.Emit()` zero-config direct use (Sync semantics)

## Performance Comparison

```bash
cd _benchmarks
go test -bench="." -benchmem -benchtime=3s -count=3 -run="^$" ./...
```

### Windows 10 — Intel Xeon E5-1650 v2 @ 3.50GHz (6C/12T)

| Scenario | beat (Sync) | beat (Async) | EventBus | × | gookit/event | × |
|----------|:---:|:---:|:---:|:---:|:---:|:---:|
| **1 handler** | **11 ns** 0 alloc | 37 ns 0 alloc | 190 ns 0 alloc | **17×** | 581 ns 2 alloc | **53×** |
| **10 handlers** | **26 ns** 0 alloc | 34 ns 0 alloc | 1690 ns 1 alloc | **65×** | 671 ns 2 alloc | **26×** |
| **Parallel** | 29 ns 0 alloc | **27 ns** 0 alloc | 255 ns 0 alloc | **9×** | 194 ns 2 alloc | **7×** |

### Alibaba Cloud Linux — Intel Xeon Platinum @ 2.50GHz (2C/2T, KVM)

| Scenario | beat (Sync) | beat (Async) | EventBus | × | gookit/event | × |
|----------|:---:|:---:|:---:|:---:|:---:|:---:|
| **1 handler** | **12 ns** 0 alloc | 30 ns 0 alloc | 140 ns 0 alloc | **11×** | 392 ns 2 alloc | **32×** |
| **10 handlers** | **19 ns** 0 alloc | 55 ns 0 alloc | 1216 ns 1 alloc | **63×** | 459 ns 2 alloc | **24×** |
| **Parallel** | **8.3 ns** 0 alloc | 30 ns 0 alloc | 175 ns 0 alloc | **21×** | 454 ns 2 alloc | **55×** |

> × = beat best / competitor (higher = faster). **Zero allocation in all scenarios**, while competitors allocate 1–2 times per Emit.
>
> Competitors: [asaskevich/EventBus](https://github.com/asaskevich/EventBus) (2k⭐) / [gookit/event](https://github.com/gookit/event) (565⭐)
>
> Full comparison code in [`_benchmarks/`](_benchmarks/) directory (separate go.mod, won't pollute main project)


## Quick Start

### Install

```bash
go get github.com/uniyakcom/beat
```

### Basic Usage (Package-Level API — Simplest)

```go
package main

import (
    "fmt"
    "github.com/uniyakcom/beat"
)

func main() {
    // Use directly, no Bus creation, no Close needed
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

> Package-level API uses Sync semantics (synchronous, returns error, no Close needed).
> For Async/Flow, use the instance API 👇

### Instance Usage (Three Paradigms)

```go
package main

import (
    "fmt"
    "github.com/uniyakcom/beat"
)

func main() {
    // Create event bus (pick one)
    bus, _ := beat.ForAsync()       // Recommended: Per-P SPSC high concurrency
    // bus, _ := beat.ForSync()     // Synchronous, returns error
    // bus, _ := beat.ForFlow()     // Pipeline batch processing
    defer bus.Close()

    // Subscribe
    id := bus.On("user.created", func(e *beat.Event) error {
        fmt.Printf("User: %s\n", string(e.Data))
        return nil
    })

    // Publish
    bus.Emit(&beat.Event{
        Type: "user.created",
        Data: []byte("alice"),
    })

    // Unsubscribe
    bus.Off(id)
}
```

## Paradigm Selection

| Paradigm | Use Case | Single-Thread | High Concurrency | Error Return | Lifecycle |
|----------|----------|--------------|-----------------|--------------|-----------|
| **Sync** | RPC, validation, hooks | **10.5 ns** | ~38 ns | ✅ | No Close needed |
| **Async** | Event bus, logging, push | 37 ns | **26 ns** | ❌ | Close required |
| **Flow** | ETL, batch loading | **48 ns** | — | ❌ | Close required |

### API Quick Reference

```go
// === Package-Level API: zero-config direct use (Sync semantics) ===
beat.On("user.created", handler)
beat.Emit(&beat.Event{Type: "user.created", Data: []byte("data")})
beat.Off(id)

// === Layer 0: New() — zero config (auto-detect optimal impl) ===
bus, _ := beat.New()        // ≥4 cores → Async, <4 cores → Sync

// === Layer 1: ForXxx() — three core APIs (recommended) ===
bus, _ := beat.ForSync()    // Sync direct call, ~10.5ns/op
bus, _ := beat.ForAsync()   // Per-P SPSC, ~26ns/op high concurrency
bus, _ := beat.ForFlow()    // Pipeline, ~48ns/op, batch window

// === Layer 2: Scenario() — string config ===
bus, _ := beat.Scenario("sync")
bus, _ := beat.Scenario("async")
bus, _ := beat.Scenario("flow")

// === Layer 3: Option() — full control ===
bus, _ := beat.Option(&beat.Profile{
    Name:  "async",
    Conc:  10000,
    TPS:   50000,
    Cores: 12,
})
```

## Core API

### Subscribe & Publish

```go
// Exact match
id := bus.On("user.created", func(e *beat.Event) error {
    return nil
})

// Single-level wildcard
bus.On("user.*", handler)    // matches user.created, user.updated

// Multi-level wildcard
bus.On("user.**", handler)   // matches user.created, user.profile.updated

// Publish
bus.Emit(&beat.Event{Type: "user.created", Data: []byte("data")})

// Publish with pattern matching
bus.EmitMatch(&beat.Event{Type: "user.created"})

// Batch publish
events := []*beat.Event{
    {Type: "user.created", Data: []byte("alice")},
    {Type: "user.created", Data: []byte("bob")},
}
bus.EmitBatch(events)

// Unsubscribe
bus.Off(id)
```

### Lifecycle Management

```go
// Sync: no Close needed (zero deps, no background goroutines)
bus, _ := beat.ForSync()

// Async/Flow: must Close (stops worker goroutines)
bus, _ := beat.ForAsync()
defer bus.Close()

// Graceful shutdown (waits for queue drain or timeout)
bus.GracefulClose(5 * time.Second)
```

## Benchmarks

Environment: Intel Xeon E5-1650 v2 @ 3.50GHz (6C/12T)

### Single-Thread

```
BenchmarkAllImpls_SingleProducer/Sync     10.42 ns/op    95.97 M/s    0 B/op    0 allocs/op
BenchmarkAllImpls_SingleProducer/Async    37.41 ns/op    26.73 M/s    0 B/op    0 allocs/op
BenchmarkAllImpls_SingleProducer/Flow     47.78 ns/op    20.94 M/s    0 B/op    0 allocs/op
```

### High Concurrency (RunParallel, 1 handler)

```
BenchmarkImplAsyncHighConcurrency/Async   26.48 ns/op    37.77 M/s    0 B/op    0 allocs/op
BenchmarkImplSyncHighConcurrency/Sync     38.37 ns/op    26.06 M/s    0 B/op    0 allocs/op
```

**Key Takeaways**:
- **Sync**: Fastest single-thread (10.5ns), CoW lock-free reads in high concurrency (~38ns)
- **Async**: Fastest high concurrency (26ns), zero CAS, Per-P SPSC ring
- **Zero Allocation**: All three paradigms 0 allocs/op on the hot path
- **Scalable**: Async concurrency scales linearly with core count

## Architecture

### Three Paradigm Implementations

| Impl | Core Technology | Use Case |
|------|----------------|----------|
| **Sync** | Direct call + CoW atomic.Pointer | RPC middleware, auth, sync hooks |
| **Async** | Per-P SPSC ring + RCU | Event bus, log aggregation, push |
| **Flow** | Pipeline + flowSnapshot + batch window | Real-time ETL, window aggregation |

### Async Architecture Details

- **Per-P SPSC Ring**: GOMAXPROCS rings (rounded up to power of 2), procPin ensures single writer
- **Worker Affinity**: worker[i] statically owns rings {i, i+w, i+2w, ...}
- **Zero CAS**: atomic Load/Store only (x86 ≈ MOV)
- **Cached Head/Tail**: producer/consumer locally cache peer progress, eliminates cross-core reads
- **RCU Subscription**: atomic.Pointer lock-free reads, CoW on write
- **Flattened Dispatch**: pre-computed `[]core.Handler`, no indirection
- **SingleKey Fast Path**: single event type skips map lookup (~16ns)
- **Three-Level Adaptive Idle**: PAUSE spin → Gosched → channel park

### Directory Structure

```
beat/
├── core/                     # Core interfaces (zero deps)
│   ├── interfaces.go        # Bus / Event / Handler
│   └── matcher.go           # TrieMatcher wildcard matching
├── internal/
│   ├── impl/                # Implementation layer (three paradigms)
│   │   ├── sync/           # Sync: direct call + CoW
│   │   ├── async/          # Async: Per-P SPSC + RCU
│   │   └── flow/           # Flow: pipeline batch
│   └── support/            # Support layer
│       ├── pool/           # Event pool + Arena
│       └── spsc/           # SPSC ring wait-free queue
├── optimize/               # Profile → Advisor → Factory
│   ├── profile.go         # Scenario config
│   ├── advisor.go         # Recommendation engine
│   └── factory.go         # Builder
├── util/                   # PerCPUCounter utilities
├── test/                   # Stress tests (separate package)
└── api.go                 # Unified API entry point
```

## Advanced Usage

### Custom Async Parameters

```go
bus, _ := beat.Option(&beat.Profile{
    Name: "async",
    // Auto-configured: Workers = NumCPU/2, RingSize = 8192
})
```

### Stats Monitoring

```go
stats := bus.Stats()
fmt.Printf("Emitted: %d, Processed: %d, Panics: %d\n",
    stats.EventsEmitted, stats.EventsProcessed, stats.Panics)
```

### Event Pool (optional)

```go
p := pool.Global()
evt := p.Acquire()
evt.Type = "user.created"
evt.Data = []byte("data")

bus.Emit(evt)
p.Release(evt)
```

## Examples

### Quick Start (Package-Level API)

```go
// No Bus creation, no Close needed, simplest usage
beat.On("order.created", func(e *beat.Event) error {
    fmt.Printf("Order: %s\n", string(e.Data))
    return nil
})

beat.Emit(&beat.Event{
    Type: "order.created",
    Data: []byte(`{"orderId":"12345"}`),
})
```

### RPC Middleware (Sync)

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

### Event Bus (Async)

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

### Log Aggregation (Async)

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

### ETL Pipeline (Flow)

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

## Best Practices

1. **Paradigm Selection**
   - Need error return → **Sync**
   - High concurrency fire-and-forget → **Async**
   - Batch data processing → **Flow**

2. **Resource Management**
   - Sync: no Close needed
   - Async/Flow: always `defer bus.Close()`

3. **Wildcards**
   - Prefer exact match (best performance)
   - `*` for single-level matching
   - `**` for multi-level matching, avoid over-matching

4. **Batch Operations**
   - Use `EmitBatch()` in high-frequency scenarios
   - Flow auto-aggregates batches

5. **Error Handling**
   - Sync: handler returns error, `Emit()` propagates directly
   - Async/Flow: handler panics auto-recover, monitor via `Stats().Panics`

## Performance Tuning

### Avoid Performance Pitfalls

- ❌ Blocking I/O in handlers (blocks Async workers)
- ❌ Excessive wildcard usage (matching on every Emit)
- ❌ Large allocations on the hot path (use event pool)
- ✅ Keep handlers lightweight, push async work to queues
- ✅ Use `EmitBatch()` for batch scenarios
- ✅ Single event type benefits from SingleKey optimization

## Development & Testing

### Test Suite

| File | Type | Description |
|------|------|-------------|
| `scenario_test.go` | Functional | Scenario integration (ForSync/ForAsync/ForFlow/Scenario/API layers) |
| `feature_error_test.go` | Functional | Error handling (single/multi handler errors, invalid Off, batch) |
| `feature_concurrent_test.go` | Functional | Concurrency safety (On/Off/Emit race, nested subscribe, concurrent Close) |
| `edge_cases_test.go` | Functional | Edge cases (zero handler, large data, special chars, slow handler) |
| `impl_bench_test.go` | Benchmark | Core regression (`BenchmarkAllImpls`, Arena, EventPool) |
| `scenario_bench_test.go` | Benchmark | Scenario-level perf (public API, single-thread + concurrent) |
| `feature_panic_bench_test.go` | Benchmark | Panic recovery & Stats overhead |
| `util/util_bench_test.go` | Benchmark | PerCPUCounter component |
| `internal/impl/flow/bus_test.go` | Unit | Flow whitebox (batch size, timeout, multi-stage, concurrency) |
| `test/stress_test.go` | Stress | Extreme conditions (1000 goroutines, 10s long-run, `-short` guarded) |

### Quick Verification

```bash
go fmt ./...                 # Format
go vet ./...                 # Static analysis
go build ./...               # Build
go test ./... -count=1       # Functional tests
go test -race ./... -short   # Race detection
```

### Benchmarks

```bash
# Key regression metrics
go test -bench="BenchmarkAllImpls" -benchtime=1s -count=1 -run=^$

# Per-implementation detailed benchmarks
go test -bench="BenchmarkImplSync$" -benchtime=3s -count=3 -run=^$
go test -bench="BenchmarkImplAsync$" -benchtime=3s -count=3 -run=^$

# CPU / memory profiling
go test -bench="BenchmarkImplFlow$" -benchtime=3s -cpuprofile=cpu.prof
go tool pprof -top cpu.prof
```

### Performance Requirements

Changes must pass `-race` and show no benchmark regression (>10%):

Environment: Intel Xeon E5-1650 v2 @ 3.50GHz (6C/12T)

| Paradigm | Single-Thread | High Concurrency | Allocations |
|----------|--------------|-----------------|-------------|
| **Sync** | ≤12 ns/op | ~38 ns/op | 0 allocs/op |
| **Async** | ~37 ns/op | ≤28 ns/op | 0 allocs/op |
| **Flow** | ~48 ns/op | — | 0 allocs/op |

## License

MIT License
