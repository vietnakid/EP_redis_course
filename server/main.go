// Lecture 9-10: same single-threaded epoll/kqueue event loop as before
// (internal/server.Server), now joined by a shared-nothing engine
// (internal/server.SharedNothingServer) - N Workers, each with a private,
// lock-free datastructure.Store, fed by N IOHandlers. -engine picks which
// one runs; both wire the same internal/shutdown package for
// SIGINT/SIGTERM -> Close(), and both take -simulate-cpu-work so the two
// can be compared I/O-bound and CPU-bound (see server/BENCHMARK.md).
package main

import (
	"flag"
	"log"
	"os"
	"runtime"

	"redis_k2/server/internal/server"
	"redis_k2/server/internal/shutdown"
)

func main() {
	engine := flag.String("engine", "shared-nothing",
		`server engine: "shared-nothing" (default, this lecture's homework) or "single" (lecture 9's event loop, kept for comparison)`)
	listeners := flag.Int("listeners", 1,
		"shared-nothing engine only: number of listener goroutines sharing :3000. N=1 is a plain Accept() loop; N>1 turns on SO_REUSEPORT so the kernel spreads new connections across all N")
	simulateCPUWork := flag.Duration("simulate-cpu-work", 0,
		"artificially slow down GET by this much (e.g. 100us), to flip the server from I/O-bound to CPU-bound for benchmarking")
	flag.Parse()

	// Ldate|Ltime|Lmicroseconds for a real timestamp, Lshortfile so every
	// log line names the file:line it came from - stdlib covers both, no
	// logging library needed for a teaching server.
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	switch *engine {
	case "single":
		srv, err := server.New(":3000", *simulateCPUWork)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("single engine started on port 3000, pid=%d", os.Getpid())
		go shutdown.WaitAndClose(srv)
		if err := srv.Run(); err != nil {
			log.Println(err)
		}
	case "shared-nothing":
		numWorkers := max(1, runtime.NumCPU()/2)
		numIOHandlers := numWorkers
		srv, err := server.NewSharedNothingServer(":3000", numWorkers, numIOHandlers, *listeners, *simulateCPUWork)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("shared-nothing engine started on port 3000 (workers=%d io_handlers=%d listeners=%d), pid=%d",
			numWorkers, numIOHandlers, *listeners, os.Getpid())
		go shutdown.WaitAndClose(srv)
		if err := srv.Run(); err != nil {
			log.Println(err)
		}
	default:
		log.Fatalf(`unknown -engine %q: want "shared-nothing" or "single"`, *engine)
	}
}
