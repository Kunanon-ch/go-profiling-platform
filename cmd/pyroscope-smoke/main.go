//go:build smoke

// Command pyroscope-smoke generates recognizable CPU, allocation, and
// goroutine profiles for the local Docker Compose acceptance test.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	profiler "profiler-probe"
)

const (
	allocationSize = 512 * 1024
	retainedSlots  = 16
)

func main() {
	started, err := profiler.StartFromEnv()
	if err != nil {
		log.Fatalf("start profiler: %v", err)
	}
	if !started {
		log.Fatal("profiler is disabled; set PYROSCOPE_ENABLED=true for the smoke test")
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	log.Println("Pyroscope smoke workload started; collect for at least 30 seconds")
	go cpuWorker(ctx)
	go allocationWorker(ctx)
	go waitingWorker(ctx)

	<-ctx.Done()
	log.Println("stopping profiler and flushing the final profile")
	if err := profiler.Stop(); err != nil && !errors.Is(err, profiler.ErrNotStarted) {
		log.Printf("stop profiler: %v", err)
	}
}

func cpuWorker(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			burnCPU(175 * time.Millisecond)
		}
	}
}

//go:noinline
func burnCPU(duration time.Duration) uint64 {
	deadline := time.Now().Add(duration)
	var value uint64 = 0x9e3779b97f4a7c15
	for time.Now().Before(deadline) {
		value = mix(value)
	}
	runtime.KeepAlive(value)
	return value
}

//go:noinline
func mix(value uint64) uint64 {
	value ^= value >> 12
	value ^= value << 25
	value ^= value >> 27
	return value * 0x2545f4914f6cdd1d
}

func allocationWorker(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	retained := make([][]byte, retainedSlots)
	index := 0
	for {
		select {
		case <-ctx.Done():
			runtime.KeepAlive(retained)
			return
		case <-ticker.C:
			retained[index] = allocateBuffer(index)
			index = (index + 1) % len(retained)
		}
	}
}

//go:noinline
func allocateBuffer(seed int) []byte {
	buffer := make([]byte, allocationSize)
	for offset := 0; offset < len(buffer); offset += 4096 {
		buffer[offset] = byte(seed + offset)
	}
	return buffer
}

// waitingWorker creates a stable, recognizable goroutine profile without
// enabling the higher-overhead block profiler.
func waitingWorker(ctx context.Context) {
	<-ctx.Done()
}
