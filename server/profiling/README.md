# CPU profiles: why shared-nothing is slower on I/O-bound GET

`BENCHMARK.md` scenario 1 shows `-engine=shared-nothing` losing to
`-engine=single` on pure I/O-bound GET, and originally *guessed* the cause was
the extra hop through a Worker (hash the key, send a `Task` over a channel,
block on a reply channel). These two profiles were captured to check that
guess against real data instead of trusting the slides - it turned out wrong,
and this is the correction.

## How to open them (flame graph + web view)

```
go tool pprof -http=:8081 server/profiling/cpu_sharednothing_get_iobound.pb.gz
go tool pprof -http=:8082 server/profiling/cpu_single_get_iobound.pb.gz
```

Opens a browser tab with the interactive graph view; the "Flame Graph" and
"Top" tabs in the left dropdown are what to show students side by side. No
extra tools needed - this is stdlib `net/http/pprof` + `go tool pprof`, both
already part of the Go toolchain.

The `.top.txt` / `.list.txt` files alongside each `.pb.gz` are the same data
as plain text, for a quick look without starting a server.

## How they were captured

Both engines got the identical load: `redis-benchmark -t get -c 500 -n 1000000
-r 1000000 --threads 3` against a keyspace pre-warmed with SET, on the same
machine as `BENCHMARK.md` (Apple M3, 8 logical CPUs). A `net/http/pprof`
endpoint was wired into `main.go` temporarily for the capture (`go func() {
http.ListenAndServe("localhost:6060", nil) }()` + `_ "net/http/pprof"`) and
reverted immediately after - it's not part of the committed server, since
this repo's servers don't ship a debug endpoint.

```
go tool pprof -proto -seconds=10 -output=out.pb.gz "http://localhost:6060/debug/pprof/profile?seconds=10"
```
run concurrently with the `redis-benchmark` call above.

## What the profiles actually show

| | flat CPU | share |
|---|---|---|
| `IOHandler.dispatch` (hash key, channel send, block on reply channel) | 0.08s / 13.30s | **0.6%** |
| `Worker.run` executing the command against its `Store` | ~0.06s / 13.30s | <1% |
| `syscall.Read` + `syscall.Write` | 11.4s / 13.30s | **90%** |

The channel handoff this lecture is teaching - and the thing `BENCHMARK.md`
originally blamed - costs almost nothing. It is not the bottleneck.

What *is* different: the same `read()`/`write()` syscalls cost more CPU per
request under shared-nothing than under the single-threaded engine, even
though they're doing identical work (same buffer size, same loopback socket,
same command). Comparing CPU-seconds spent against requests actually served
in the same wall-clock window:

- single engine: 3.75 CPU-s for ~2.0M requests -> **~1.9µs of CPU per request**
- shared-nothing: 13.30 CPU-s for ~1.45M requests -> **~9.2µs of CPU per request**

OS thread counts were nearly identical between the two runs (15 vs 16 via
`ps -M <pid>`), so it isn't "shared-nothing spins up more threads that fight
for CPU" either. What's left: shared-nothing runs 4 `IOHandler` goroutines,
each its own kqueue loop, each independently blocking in `Read`/`Write` on its
own slice of connections - i.e. 4 OS threads issuing concurrent syscalls
against the same loopback network stack at once. The single-threaded engine
never has two threads calling `read()`/`write()` at the same instant. The
extra CPU cost is kernel-side contention from parallel syscalls on shared
kernel state (socket buffers, the TCP/IP stack), not user-space dispatch
overhead.

This is also *why* scenario 2 (`-simulate-cpu-work=100us`) flips the result:
once there's real CPU work per request, the 4 Workers running it in parallel
wins far more than the syscall contention costs - the payoff shows up exactly
where the lecture says it should, just not for the reason `BENCHMARK.md`
originally gave for scenario 1's regression.
