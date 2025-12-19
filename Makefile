.PHONY: help test test-race test-verbose test-short test-cover stress stress-race stress-all bench build clean lint fmt vet gosec install bench-uniform bench-skewed bench-priority bench-burst bench-small-pool bench-all

# Variables
BINARY_NAME=poolme
GO=go
GOTEST=$(GO) test
GOBUILD=$(GO) build
GOMOD=$(GO) mod
STRESS_COUNT?=10
TIMEOUT?=30s
PACKAGES=$(shell $(GO) list ./... | grep -v /examples)
TEST_PACKAGES=$(shell $(GO) list ./... | grep -v /examples | grep -v /benchmarks)

# Colors for output
BLUE=\033[0;34m
GREEN=\033[0;32m
RED=\033[0;31m
YELLOW=\033[1;33m
NC=\033[0m # No Color

## help: Display this help message
help:
	@echo "$(BLUE)PoolMe Makefile Commands$(NC)"
	@echo ""
	@echo "$(GREEN)Testing:$(NC)"
	@echo "  make test              - Run all tests"
	@echo "  make test-race         - Run all tests with race detector"
	@echo "  make test-verbose      - Run tests with verbose output"
	@echo "  make test-short        - Run tests in short mode"
	@echo "  make test-cover        - Run tests with coverage report"
	@echo ""
	@echo "$(GREEN)Stress Testing (for concurrency):$(NC)"
	@echo "  make stress            - Run all tests $(STRESS_COUNT) times (set STRESS_COUNT=N)"
	@echo "  make stress-race       - Run all tests $(STRESS_COUNT) times with race detector"
	@echo "  make stress-all        - Run stress test on all packages individually"
	@echo "  make stress-pool       - Stress test only the pool package"
	@echo "  make stress-scheduler  - Stress test only the scheduler package"
	@echo ""
	@echo "$(GREEN)Benchmarking:$(NC)"
	@echo "  make bench             - Run all benchmarks"
	@echo "  make bench-compare     - Run benchmarks and save results for comparison"
	@echo ""
	@echo "$(GREEN)Building:$(NC)"
	@echo "  make build             - Build the project"
	@echo "  make install           - Install dependencies"
	@echo ""
	@echo "$(GREEN)Code Quality:$(NC)"
	@echo "  make lint              - Run golangci-lint"
	@echo "  make fmt               - Format code with gofmt"
	@echo "  make vet               - Run go vet"
	@echo "  make gosec             - Run gosec security scanner"
	@echo "  make check             - Run fmt, vet, lint, and gosec"
	@echo ""
	@echo "$(GREEN)Benchmark Scenarios (Workload Comparisons):$(NC)"
	@echo "  make bench-uniform      - Scenario 1: Uniform throughput (500K tasks)"
	@echo "  make bench-skewed       - Scenario 2: Imbalanced workload (200K tasks)"
	@echo "  make bench-priority     - Scenario 3: Priority ordering (100K tasks)"
	@echo "  make bench-burst        - Scenario 4: High-concurrency stress (1M tasks)"
	@echo "  make bench-small-pool   - Scenario 5: Small worker pool (500K tasks, 4 workers)"
	@echo "  make bench-all          - Run all 5 benchmark scenarios"
	@echo ""
	@echo "$(GREEN)Utilities:$(NC)"
	@echo "  make clean             - Clean build artifacts and test cache"
	@echo "  make tidy              - Tidy go modules"
	@echo ""

## test: Run all tests
test:
	@echo "$(BLUE)Running tests...$(NC)"
	$(GOTEST) -timeout $(TIMEOUT) $(TEST_PACKAGES)

## test-race: Run all tests with race detector
test-race:
	@echo "$(BLUE)Running tests with race detector...$(NC)"
	$(GOTEST) -race -timeout $(TIMEOUT) $(TEST_PACKAGES)

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "$(BLUE)Running tests (verbose)...$(NC)"
	$(GOTEST) -v -timeout $(TIMEOUT) $(TEST_PACKAGES)

## test-short: Run tests in short mode
test-short:
	@echo "$(BLUE)Running tests (short mode)...$(NC)"
	$(GOTEST) -short -timeout $(TIMEOUT) $(TEST_PACKAGES)

## test-cover: Run tests with coverage
test-cover:
	@echo "$(BLUE)Running tests with coverage...$(NC)"
	$(GOTEST) -cover -coverprofile=coverage.out $(TEST_PACKAGES)
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

## stress: Run all tests multiple times to catch race conditions
stress:
	@echo "$(BLUE)Running stress test ($(STRESS_COUNT) iterations)...$(NC)"
	@for i in $$(seq 1 $(STRESS_COUNT)); do \
		echo "$(YELLOW)=== Iteration $$i/$(STRESS_COUNT) ===$(NC)"; \
		$(GOTEST) -timeout $(TIMEOUT) $(TEST_PACKAGES) || exit 1; \
	done
	@echo "$(GREEN)All $(STRESS_COUNT) iterations passed!$(NC)"

## stress-race: Run all tests multiple times with race detector
stress-race:
	@echo "$(BLUE)Running stress test with race detector ($(STRESS_COUNT) iterations)...$(NC)"
	@for i in $$(seq 1 $(STRESS_COUNT)); do \
		echo "$(YELLOW)=== Iteration $$i/$(STRESS_COUNT) ===$(NC)"; \
		$(GOTEST) -race -timeout $(TIMEOUT) $(TEST_PACKAGES) || exit 1; \
	done
	@echo "$(GREEN)All $(STRESS_COUNT) iterations passed with race detector!$(NC)"

## stress-all: Run stress test on each package individually
stress-all:
	@echo "$(BLUE)Running stress test on all packages individually...$(NC)"
	@for pkg in $(TEST_PACKAGES); do \
		echo "$(YELLOW)=== Testing package: $$pkg ===$(NC)"; \
		for i in $$(seq 1 $(STRESS_COUNT)); do \
			echo "$(YELLOW)  Iteration $$i/$(STRESS_COUNT)$(NC)"; \
			$(GOTEST) -race -timeout $(TIMEOUT) $$pkg || exit 1; \
		done; \
		echo "$(GREEN)  Package $$pkg: All $(STRESS_COUNT) iterations passed!$(NC)"; \
	done
	@echo "$(GREEN)All packages passed stress test!$(NC)"

## stress-pool: Stress test only the pool package
stress-pool:
	@echo "$(BLUE)Running stress test on pool package ($(STRESS_COUNT) iterations)...$(NC)"
	@for i in $$(seq 1 $(STRESS_COUNT)); do \
		echo "$(YELLOW)=== Iteration $$i/$(STRESS_COUNT) ===$(NC)"; \
		$(GOTEST) -race -timeout $(TIMEOUT) github.com/utkarsh5026/poolme/pool || exit 1; \
	done
	@echo "$(GREEN)Pool package: All $(STRESS_COUNT) iterations passed!$(NC)"

## stress-scheduler: Stress test only the scheduler package
stress-scheduler:
	@echo "$(BLUE)Running stress test on scheduler package ($(STRESS_COUNT) iterations)...$(NC)"
	@for i in $$(seq 1 $(STRESS_COUNT)); do \
		echo "$(YELLOW)=== Iteration $$i/$(STRESS_COUNT) ===$(NC)"; \
		$(GOTEST) -race -timeout $(TIMEOUT) github.com/utkarsh5026/poolme/internal/scheduler || exit 1; \
	done
	@echo "$(GREEN)Scheduler package: All $(STRESS_COUNT) iterations passed!$(NC)"

## bench: Run all benchmarks
bench:
	@echo "$(BLUE)Running benchmarks...$(NC)"
	$(GOTEST) -bench=. -benchmem -run=^$$ $(PACKAGES)

## bench-compare: Run benchmarks and save results for comparison
bench-compare:
	@echo "$(BLUE)Running benchmarks and saving results...$(NC)"
	$(GOTEST) -bench=. -benchmem -run=^$$ $(PACKAGES) | tee benchmark_results.txt
	@echo "$(GREEN)Benchmark results saved to benchmark_results.txt$(NC)"

## build: Build the project
build:
	@echo "$(BLUE)Building $(BINARY_NAME)...$(NC)"
	$(GOBUILD) -v ./...
	@echo "$(GREEN)Build complete!$(NC)"

## install: Install dependencies
install:
	@echo "$(BLUE)Installing dependencies...$(NC)"
	$(GOMOD) download
	$(GOMOD) verify
	@echo "$(GREEN)Dependencies installed!$(NC)"

## lint: Run golangci-lint
lint:
	@echo "$(BLUE)Running golangci-lint...$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "$(RED)golangci-lint not found. Install it from https://golangci-lint.run/$(NC)"; \
		exit 1; \
	fi

## fmt: Format code
fmt:
	@echo "$(BLUE)Formatting code...$(NC)"
	$(GO) fmt $(PACKAGES)
	@echo "$(GREEN)Code formatted!$(NC)"

## vet: Run go vet
vet:
	@echo "$(BLUE)Running go vet...$(NC)"
	$(GO) vet $(PACKAGES)
	@echo "$(GREEN)Vet complete!$(NC)"

## gosec: Run gosec security scanner
gosec:
	@echo "$(BLUE)Running gosec security scanner...$(NC)"
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "$(RED)gosec not found. Install it with: go install github.com/securego/gosec/v2/cmd/gosec@latest$(NC)"; \
		exit 1; \
	fi

## check: Run fmt, vet, lint, and gosec
check: fmt vet lint gosec
	@echo "$(GREEN)All checks passed!$(NC)"

## clean: Clean build artifacts and test cache
clean:
	@echo "$(BLUE)Cleaning...$(NC)"
	$(GO) clean
	$(GO) clean -testcache
	rm -f coverage.out coverage.html
	rm -f benchmark_results.txt
	@echo "$(GREEN)Clean complete!$(NC)"

## tidy: Tidy go modules
tidy:
	@echo "$(BLUE)Tidying go modules...$(NC)"
	$(GOMOD) tidy
	@echo "$(GREEN)Modules tidied!$(NC)"


## ═══════════════════════════════════════════════════════════
## Benchmark Scenarios - Different workload patterns to show strategy strengths
## ═══════════════════════════════════════════════════════════

## bench-uniform: Scenario 1 - Uniform high-throughput baseline
bench-uniform:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Scenario 1: Uniform High-Throughput Baseline             ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)📊 Workload: 500K tasks, all identical complexity (pure throughput)$(NC)"
	@cd examples/real-world/bench/runner && $(GO) run runner.go -tasks=500000 -complexity=50 -workload=balanced
	@echo ""

## bench-skewed: Scenario 2 - Highly skewed workload (load balancing test)
bench-skewed:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Scenario 2: Highly Skewed Workload (Load Balancing)      ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)📊 Workload: 200K tasks with 10%% heavy, 20%% medium, 70%% light (extreme imbalance)$(NC)"
	@cd examples/real-world/bench/runner && $(GO) run runner.go -tasks=200000 -complexity=500 -workload=imbalanced
	@echo ""

## bench-priority: Scenario 3 - Priority-ordered processing (reordering test)
bench-priority:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Scenario 3: Priority-Ordered Processing                  ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)📊 Workload: 100K tasks in reverse order (test priority scheduler reordering)$(NC)"
	@cd examples/real-world/bench/runner && $(GO) run runner.go -tasks=100000 -complexity=5000 -workload=priority
	@echo ""

## bench-burst: Scenario 4 - High-concurrency stress test
bench-burst:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Scenario 4: High-Concurrency Stress Test                 ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)📊 Workload: 1M tasks with balanced complexity (stress test)$(NC)"
	@cd examples/real-world/bench/runner && $(GO) run runner.go -tasks=1000000 -complexity=1000 -workload=balanced
	@echo ""

## bench-small-pool: Scenario 5 - Small worker pool (4 workers)
bench-small-pool:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Scenario 5: Small Worker Pool Efficiency                 ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)📊 Workload: 500K tasks with only 4 workers (low contention test)$(NC)"
	@cd examples/real-world/bench/runner && $(GO) run runner.go -tasks=500000 -complexity=5000 -workload=balanced -workers=4
	@echo ""

## bench-all: Run all 5 benchmark scenarios sequentially
bench-all:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║        Running All 5 Scheduler Benchmark Scenarios         ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@$(MAKE) bench-uniform
	@echo ""
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) bench-skewed
	@echo ""
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) bench-priority
	@echo ""
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) bench-burst
	@echo ""
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) bench-small-pool
	@echo ""
	@echo "$(GREEN)✅ All 5 benchmark scenarios complete!$(NC)"
	@echo ""