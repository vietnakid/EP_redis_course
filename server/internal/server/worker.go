package server

import (
	"time"

	"redis_k2/server/internal/command"
	"redis_k2/server/internal/datastructure"
	"redis_k2/server/internal/protocol"
)

// workerActiveExpireInterval mirrors activeExpireInterval on the
// single-threaded Server: how often an idle Worker sweeps its own Store for
// expired keys. A Worker has no syscall.Wait() to piggyback a timeout on
// (unlike an IOHandler), so a ticker is the natural fit here instead.
const workerActiveExpireInterval = 100 * time.Millisecond

// Task is one parsed command an IOHandler has routed to a Worker, plus the
// channel to send the reply back on.
type Task struct {
	Cmd     protocol.Command
	ReplyCh chan []byte
}

// Worker owns one private *datastructure.Store and is the only goroutine
// that ever touches it. Shared-nothing means no two Workers' Stores are
// ever the same map, so nothing here needs a lock - contrast the
// single-threaded Server, which also owns exactly one Store but for the
// opposite reason (only one goroutine exists at all).
type Worker struct {
	id              int
	store           *datastructure.Store
	simulateCPUWork time.Duration
	TaskCh          chan *Task
}

// NewWorker starts the Worker's own goroutine immediately - it just blocks
// on an empty TaskCh until dispatch sends it work, so this is cheap and
// keeps every Worker's lifetime tied to the process, not to its first Task.
func NewWorker(id int, taskBufferSize int, simulateCPUWork time.Duration) *Worker {
	w := &Worker{
		id:              id,
		store:           datastructure.NewStore(),
		simulateCPUWork: simulateCPUWork,
		TaskCh:          make(chan *Task, taskBufferSize),
	}
	go w.run()
	return w
}

// run is the Worker's entire life: execute whatever Task arrives against
// its own Store, reply, repeat - until TaskCh is closed (Close's doing),
// at which point it drains whatever was already buffered and returns.
func (w *Worker) run() {
	ticker := time.NewTicker(workerActiveExpireInterval)
	defer ticker.Stop()
	for {
		select {
		case task, ok := <-w.TaskCh:
			if !ok {
				return
			}
			simulateCPUWorkOnGet(task.Cmd, w.simulateCPUWork)
			task.ReplyCh <- command.Handle(w.store, task.Cmd)
		case <-ticker.C:
			w.store.ActiveExpireCycle()
		}
	}
}
