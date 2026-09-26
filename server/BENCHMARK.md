# Lecture 9-10 benchmark: shared-nothing vs single-threaded

Measures what `-engine=shared-nothing` (this lecture's homework) actually
buys over `-engine=single` (lecture 9's event loop, kept for comparison),
and whether `-listeners=N` (`SO_REUSEPORT`) is worth it on top. Reproduce
with `redis-benchmark` from a local Redis checkout; the two commands below
are lifted straight from the lecture slides.

```
redis-benchmark -p 3000 -t set -n 1000000 -r 1000000
redis-benchmark -n 1000000 -t get -c 500 -h localhost -p 3000 -r 1000000 --threads 3
```

## Machine

Apple M3, macOS 27.2, Go 1.26.5 darwin/arm64, 8 logical CPUs - so
`-engine=shared-nothing`'s default is `numWorkers = numIOHandlers =
runtime.NumCPU()/2 = 4`. Server and `redis-benchmark` both ran on the same
machine (loopback), one scenario at a time.

## 1. I/O-bound: single vs shared-nothing

No `-simulate-cpu-work` - GET/SET do essentially no CPU work, so this
measures pure event-loop/dispatch overhead.

| Engine                          | SET rps   | GET rps   |
|----------------------------------|-----------|-----------|
| `-engine=single`                 | 204,666   | 221,976   |
| `-engine=shared-nothing -listeners=1` | 193,911   | 181,291   |

Shared-nothing is *slower* here, not faster - matching the lecture's own
conclusion (slides 29-31) that an I/O-bound workload doesn't benefit from
extra worker goroutines, though not for the reason first assumed here. CPU
profiling (see `profiling/README.md`) shows the channel hop to a Worker and
back costs under 1% of CPU time - negligible. The real cost is that
`syscall.Read`/`syscall.Write` themselves get ~5x more expensive per request
under shared-nothing (~9.2µs CPU/request vs ~1.9µs single-threaded), because
4 `IOHandler` goroutines are now issuing those syscalls concurrently against
the same loopback network stack, and the kernel-side contention that creates
outweighs whatever the extra goroutines could parallelize. There's nothing
to parallelize yet when the bottleneck is syscalls, not CPU - see
`profiling/README.md` for the actual measurement.

## 2. CPU-bound: single vs shared-nothing

`-simulate-cpu-work=100us` added to both engines - GET now spends 100µs
"working" before replying, flipping the server from I/O-bound to
CPU-bound (same trick the lecture uses: slides 31-32).

| Engine                          | SET rps   | GET rps   |
|----------------------------------|-----------|-----------|
| `-engine=single`                 | 202,020   | 7,320     |
| `-engine=shared-nothing -listeners=1` | 181,653   | 21,382    |

GET throughput is now the whole story: single-threaded serializes every
100µs of "work" through one goroutine (theoretical ceiling ~10,000 rps;
7,320 measured once RESP parsing and syscalls are added in). Shared-nothing
spreads that same work across 4 Workers and lands **~2.9x** the GET
throughput (21,382 vs 7,320) - this is the payoff shared-nothing actually
exists for, and it only shows up once the server has real CPU work to
parallelize. SET rps barely moves in either engine (SET isn't slowed by
`-simulate-cpu-work`, which only targets GET, matching the lecture).

## 3. `SO_REUSEPORT`: 1 listener vs 4 listeners

Both shared-nothing, I/O-bound, 4 Workers/IOHandlers held fixed - only
`-listeners` changes.

| Listeners | SET rps   | GET rps   |
|-----------|-----------|-----------|
| 1         | 193,911   | 181,291   |
| 4         | 197,200   | 181,159   |

No meaningful difference on this machine/workload: a single listener
goroutine's blocking `Accept()` loop was never the bottleneck here, so
`SO_REUSEPORT`'s kernel-side load-balancing has nothing to relieve.
`-listeners=4` is there to *demonstrate the mechanism* (verified by
`io_handler.go`'s log lines showing connections landing across every
IOHandler when `-listeners=4`), not because this workload needed it - the
lecture poses it as a question (slide 22: "Just one problem"), not a
proven win, and this is the honest answer for a loopback single-box
benchmark.

## Takeaway

Shared-nothing's cost (channel handoff, reply-channel round trip) is real
and shows up as a small I/O-bound regression; its benefit (real
parallelism across Workers) is also real, but only once there's CPU work
to parallelize. Neither result is a bug - it's the same conclusion the
lecture draws from its own profiling, reproduced here against this repo's
own implementation instead of the reference one.
