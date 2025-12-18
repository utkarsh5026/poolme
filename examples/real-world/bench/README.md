# Benchmark Profiling

This directory contains a comprehensive profiling setup for benchmarking different scheduling strategies in the worker pool.

## Quick Start

```bash
cd examples/real-world/bench

# View all available commands
make help

# Profile a specific strategy
make profile-work-stealing

# View the CPU profile in your browser
make profile-view-cpu-work-stealing

# View the memory profile in your browser
make profile-view-mem-work-stealing
```

## Available Strategies

The following scheduling strategies can be profiled:

- **Channel** - Default Go channel-based scheduler
- **Work-Stealing** - Work-stealing algorithm for load balancing
- **MPMC Queue** - Multi-Producer Multi-Consumer queue
- **LMAX Disruptor** - High-performance ring buffer pattern
- **Priority Queue** - Priority-based task scheduling
- **Skip List** - Probabilistic ordered data structure
- **Bitmask** - Bitmap-based task tracking

## Profiling Commands

### Profile Individual Strategies

```bash
make profile-channel         # Profile Channel strategy
make profile-work-stealing   # Profile Work-Stealing strategy
make profile-mpmc            # Profile MPMC Queue strategy
make profile-lmax            # Profile LMAX Disruptor strategy
make profile-priority-queue  # Profile Priority Queue strategy
make profile-skip-list       # Profile Skip List strategy
make profile-bitmask         # Profile Bitmask strategy
```

Each command generates both CPU and memory profiles in the `profiles/` directory.

### Profile All Strategies

```bash
make profile-all
```

This runs profiling for all 7 strategies sequentially. This will take several minutes to complete.

### View Profiles

After profiling, you can visualize the results in your browser:

```bash
# View CPU profiles
make profile-view-cpu-channel
make profile-view-cpu-work-stealing
make profile-view-cpu-mpmc
make profile-view-cpu-lmax
make profile-view-cpu-priority-queue
make profile-view-cpu-skip-list
make profile-view-cpu-bitmask

# View memory profiles
make profile-view-mem-channel
make profile-view-mem-work-stealing
make profile-view-mem-mpmc
make profile-view-mem-lmax
make profile-view-mem-priority-queue
make profile-view-mem-skip-list
make profile-view-mem-bitmask
```

The pprof web interface will open at `http://localhost:8080` by default.

### Clean Up

```bash
make profile-clean
```

Removes all generated profile files.

## Configuration

You can customize the profiling workload using environment variables:

```bash
# Change the number of rows to process (default: 10,000,000)
PROFILE_ROWS=50000000 make profile-mpmc

# Change the chunk size (default: 5,000)
PROFILE_CHUNK=10000 make profile-work-stealing

# Change the pprof web UI port (default: 8080)
PROFILE_PORT=9090 make profile-view-cpu STRATEGY=channel
```

### Multiple Variables

```bash
PROFILE_ROWS=100000000 PROFILE_CHUNK=10000 make profile-lmax
```

## Understanding Profile Files

Each profiling run generates two files:

- `{strategy}_cpu.prof` - CPU profiling data showing where time is spent
- `{strategy}_mem.prof` - Memory profiling data showing allocations

### Profile File Naming

- `channel_cpu.prof` / `channel_mem.prof`
- `work-stealing_cpu.prof` / `work-stealing_mem.prof`
- `mpmc_cpu.prof` / `mpmc_mem.prof`
- `lmax_cpu.prof` / `lmax_mem.prof`
- `priority-queue_cpu.prof` / `priority-queue_mem.prof`
- `skip-list_cpu.prof` / `skip-list_mem.prof`
- `bitmask_cpu.prof` / `bitmask_mem.prof`

## Using pprof Web UI

When you run `make profile-view-cpu` or `make profile-view-mem`, the pprof web interface opens with several views:

### Top View

Shows functions consuming the most CPU time or memory.

### Graph View

Visual representation of the call graph showing where time/memory is being used.

### Flame Graph

Interactive flame graph for visualizing the call stack.

### Peek

Source code view with annotations showing hotspots.

### Source

View source code with line-by-line profiling data.

## Advanced Usage

### Command-Line pprof

You can also use pprof from the command line:

```bash
# Interactive mode
go tool pprof profiles/work-stealing_cpu.prof

# Top 10 functions by CPU time
go tool pprof -top profiles/work-stealing_cpu.prof

# Generate SVG call graph
go tool pprof -svg profiles/work-stealing_cpu.prof > cpu_graph.svg

# Compare two profiles
go tool pprof -base=profiles/channel_cpu.prof profiles/work-stealing_cpu.prof
```

### Comparing Strategies

To compare different strategies:

1. Profile both strategies:

   ```bash
   make profile-channel
   make profile-work-stealing
   ```

2. Compare using pprof:

   ```bash
   go tool pprof -base=profiles/channel_cpu.prof profiles/work-stealing_cpu.prof
   ```

This shows the difference in CPU usage between the two strategies.

## Example Workflow

```bash
# 1. Navigate to the bench directory
cd examples/real-world/bench

# 2. Profile the Work-Stealing strategy with custom workload
PROFILE_ROWS=50000000 PROFILE_CHUNK=10000 make profile-work-stealing

# 3. View the CPU profile in browser
make profile-view-cpu-work-stealing

# 4. Analyze the flame graph and identify hotspots

# 5. Compare with Channel strategy
make profile-channel
go tool pprof -base=profiles/channel_cpu.prof profiles/work-stealing_cpu.prof

# 6. Clean up when done
make profile-clean
```

## Troubleshooting

### Port Already in Use

If port 8080 is already in use:

```bash
PROFILE_PORT=9090 make profile-view-cpu-work-stealing
```

### Profile File Not Found

Make sure you've run the profiling command first:

```bash
make profile-work-stealing
```

Then view it:

```bash
make profile-view-cpu-work-stealing
```

### Windows Users

If you're on Windows and the Makefile commands don't work, you can run the Go commands directly:

```bash
# Profile (use "." to compile all files in the directory)
go run . -strategy="Work-Stealing" -rows=10000000 -chunk=5000 -cpuprofile=profiles/work-stealing_cpu.prof -memprofile=profiles/work-stealing_mem.prof

# View
go tool pprof -http=:8080 profiles/work-stealing_cpu.prof
```

**Note:** Use `go run .` instead of `go run main.go` to ensure all files (including platform-specific terminal handlers) are compiled.

## See Also

- [Go pprof Documentation](https://pkg.go.dev/runtime/pprof)
- [Profiling Go Programs](https://go.dev/blog/pprof)
- [Main Benchmark Tool](./main.go)
