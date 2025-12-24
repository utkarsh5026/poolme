.PHONY: help profile-channel profile-work-stealing profile-mpmc profile-lmax profile-priority-queue profile-skip-list profile-bitmask profile-all profile-view-cpu-channel profile-view-cpu-work-stealing profile-view-cpu-mpmc profile-view-cpu-lmax profile-view-cpu-priority-queue profile-view-cpu-skip-list profile-view-cpu-bitmask profile-view-mem-channel profile-view-mem-work-stealing profile-view-mem-mpmc profile-view-mem-lmax profile-view-mem-priority-queue profile-view-mem-skip-list profile-view-mem-bitmask profile-clean

# Variables
GO=go
PROFILE_DIR=profiles
PROFILE_TASKS?=100000
PROFILE_COMPLEXITY?=10000
PROFILE_PORT?=8080

# Colors for output
BLUE=\033[0;34m
GREEN=\033[0;32m
RED=\033[0;31m
YELLOW=\033[1;33m
NC=\033[0m # No Color

## help: Display this help message
help:
	@echo "$(BLUE)┌────────────────────────────────────────────────────────────┐$(NC)"
	@echo "$(BLUE)│           Benchmark Profiling Commands                    │$(NC)"
	@echo "$(BLUE)└────────────────────────────────────────────────────────────┘$(NC)"
	@echo ""
	@echo "$(GREEN)Profile Individual Strategies:$(NC)"
	@echo "  make profile-channel         - Profile Channel strategy"
	@echo "  make profile-work-stealing   - Profile Work-Stealing strategy"
	@echo "  make profile-mpmc            - Profile MPMC Queue strategy"
	@echo "  make profile-lmax            - Profile LMAX Disruptor strategy"
	@echo "  make profile-priority-queue  - Profile Priority Queue strategy"
	@echo "  make profile-skip-list       - Profile Skip List strategy"
	@echo "  make profile-bitmask         - Profile Bitmask strategy"
	@echo ""
	@echo "$(GREEN)Bulk Operations:$(NC)"
	@echo "  make profile-all                  - Profile all strategies sequentially"
	@echo ""
	@echo "$(GREEN)View CPU Profiles (opens in browser):$(NC)"
	@echo "  make profile-view-cpu-channel"
	@echo "  make profile-view-cpu-work-stealing"
	@echo "  make profile-view-cpu-mpmc"
	@echo "  make profile-view-cpu-lmax"
	@echo "  make profile-view-cpu-priority-queue"
	@echo "  make profile-view-cpu-skip-list"
	@echo "  make profile-view-cpu-bitmask"
	@echo ""
	@echo "$(GREEN)View Memory Profiles (opens in browser):$(NC)"
	@echo "  make profile-view-mem-channel"
	@echo "  make profile-view-mem-work-stealing"
	@echo "  make profile-view-mem-mpmc"
	@echo "  make profile-view-mem-lmax"
	@echo "  make profile-view-mem-priority-queue"
	@echo "  make profile-view-mem-skip-list"
	@echo "  make profile-view-mem-bitmask"
	@echo ""
	@echo "$(GREEN)Utilities:$(NC)"
	@echo "  make profile-clean                - Clean all profile files"
	@echo ""
	@echo "$(YELLOW)Configuration (environment variables):$(NC)"
	@echo "  PROFILE_TASKS=$(PROFILE_TASKS)       - Number of tasks to process"
	@echo "  PROFILE_COMPLEXITY=$(PROFILE_COMPLEXITY) - CPU work per task"
	@echo "  PROFILE_PORT=$(PROFILE_PORT)            - Port for pprof web UI"
	@echo ""
	@echo "$(YELLOW)Example:$(NC)"
	@echo "  make profile-work-stealing"
	@echo "  make profile-view-cpu-work-stealing"
	@echo "  PROFILE_TASKS=500000 make profile-mpmc"
	@echo ""

## ═══════════════════════════════════════════════════════════
## CPU & Memory Profiling - Profile individual strategies
## ═══════════════════════════════════════════════════════════

## profile-channel: Profile Channel strategy (CPU + Memory)
profile-channel:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Profiling: Channel Strategy                               ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@mkdir -p $(PROFILE_DIR)
	@$(GO) run . \
		-strategy="Channel" \
		-tasks=$(PROFILE_TASKS) \
		-complexity=$(PROFILE_COMPLEXITY) \
		-cpuprofile=$(PROFILE_DIR)/channel_cpu.prof \
		-memprofile=$(PROFILE_DIR)/channel_mem.prof
	@echo ""
	@echo "$(GREEN)✅ Profiles saved:$(NC)"
	@echo "  CPU: $(PROFILE_DIR)/channel_cpu.prof"
	@echo "  MEM: $(PROFILE_DIR)/channel_mem.prof"
	@echo ""
	@echo "$(YELLOW)View with:$(NC)"
	@echo "  make profile-view-cpu-channel"
	@echo "  make profile-view-mem-channel"
	@echo ""

## profile-work-stealing: Profile Work-Stealing strategy (CPU + Memory)
profile-work-stealing:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Profiling: Work-Stealing Strategy                         ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@mkdir -p $(PROFILE_DIR)
	@$(GO) run . \
		-strategy="Work-Stealing" \
		-tasks=$(PROFILE_TASKS) \
		-complexity=$(PROFILE_COMPLEXITY) \
		-cpuprofile=$(PROFILE_DIR)/work-stealing_cpu.prof \
		-memprofile=$(PROFILE_DIR)/work-stealing_mem.prof
	@echo ""
	@echo "$(GREEN)✅ Profiles saved:$(NC)"
	@echo "  CPU: $(PROFILE_DIR)/work-stealing_cpu.prof"
	@echo "  MEM: $(PROFILE_DIR)/work-stealing_mem.prof"
	@echo ""
	@echo "$(YELLOW)View with:$(NC)"
	@echo "  make profile-view-cpu-work-stealing"
	@echo "  make profile-view-mem-work-stealing"
	@echo ""

## profile-mpmc: Profile MPMC Queue strategy (CPU + Memory)
profile-mpmc:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Profiling: MPMC Queue Strategy                            ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@mkdir -p $(PROFILE_DIR)
	@$(GO) run . \
		-strategy="MPMC Queue" \
		-tasks=$(PROFILE_TASKS) \
		-complexity=$(PROFILE_COMPLEXITY) \
		-cpuprofile=$(PROFILE_DIR)/mpmc_cpu.prof \
		-memprofile=$(PROFILE_DIR)/mpmc_mem.prof
	@echo ""
	@echo "$(GREEN)✅ Profiles saved:$(NC)"
	@echo "  CPU: $(PROFILE_DIR)/mpmc_cpu.prof"
	@echo "  MEM: $(PROFILE_DIR)/mpmc_mem.prof"
	@echo ""
	@echo "$(YELLOW)View with:$(NC)"
	@echo "  make profile-view-cpu-mpmc"
	@echo "  make profile-view-mem-mpmc"
	@echo ""

## profile-lmax: Profile LMAX Disruptor strategy (CPU + Memory)
profile-lmax:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Profiling: LMAX Disruptor Strategy                        ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@mkdir -p $(PROFILE_DIR)
	@$(GO) run . \
		-strategy="LMAX Disruptor" \
		-tasks=$(PROFILE_TASKS) \
		-complexity=$(PROFILE_COMPLEXITY) \
		-cpuprofile=$(PROFILE_DIR)/lmax_cpu.prof \
		-memprofile=$(PROFILE_DIR)/lmax_mem.prof
	@echo ""
	@echo "$(GREEN)✅ Profiles saved:$(NC)"
	@echo "  CPU: $(PROFILE_DIR)/lmax_cpu.prof"
	@echo "  MEM: $(PROFILE_DIR)/lmax_mem.prof"
	@echo ""
	@echo "$(YELLOW)View with:$(NC)"
	@echo "  make profile-view-cpu-lmax"
	@echo "  make profile-view-mem-lmax"
	@echo ""

## profile-priority-queue: Profile Priority Queue strategy (CPU + Memory)
profile-priority-queue:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Profiling: Priority Queue Strategy                        ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@mkdir -p $(PROFILE_DIR)
	@$(GO) run . \
		-strategy="Priority Queue" \
		-tasks=$(PROFILE_TASKS) \
		-complexity=$(PROFILE_COMPLEXITY) \
		-cpuprofile=$(PROFILE_DIR)/priority-queue_cpu.prof \
		-memprofile=$(PROFILE_DIR)/priority-queue_mem.prof
	@echo ""
	@echo "$(GREEN)✅ Profiles saved:$(NC)"
	@echo "  CPU: $(PROFILE_DIR)/priority-queue_cpu.prof"
	@echo "  MEM: $(PROFILE_DIR)/priority-queue_mem.prof"
	@echo ""
	@echo "$(YELLOW)View with:$(NC)"
	@echo "  make profile-view-cpu-priority-queue"
	@echo "  make profile-view-mem-priority-queue"
	@echo ""

## profile-skip-list: Profile Skip List strategy (CPU + Memory)
profile-skip-list:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Profiling: Skip List Strategy                             ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@mkdir -p $(PROFILE_DIR)
	@$(GO) run . \
		-strategy="Skip List" \
		-tasks=$(PROFILE_TASKS) \
		-complexity=$(PROFILE_COMPLEXITY) \
		-cpuprofile=$(PROFILE_DIR)/skip-list_cpu.prof \
		-memprofile=$(PROFILE_DIR)/skip-list_mem.prof
	@echo ""
	@echo "$(GREEN)✅ Profiles saved:$(NC)"
	@echo "  CPU: $(PROFILE_DIR)/skip-list_cpu.prof"
	@echo "  MEM: $(PROFILE_DIR)/skip-list_mem.prof"
	@echo ""
	@echo "$(YELLOW)View with:$(NC)"
	@echo "  make profile-view-cpu-skip-list"
	@echo "  make profile-view-mem-skip-list"
	@echo ""

## profile-bitmask: Profile Bitmask strategy (CPU + Memory)
profile-bitmask:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║  Profiling: Bitmask Strategy                               ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@mkdir -p $(PROFILE_DIR)
	@$(GO) run . \
		-strategy="Bitmask" \
		-tasks=$(PROFILE_TASKS) \
		-complexity=$(PROFILE_COMPLEXITY) \
		-cpuprofile=$(PROFILE_DIR)/bitmask_cpu.prof \
		-memprofile=$(PROFILE_DIR)/bitmask_mem.prof
	@echo ""
	@echo "$(GREEN)✅ Profiles saved:$(NC)"
	@echo "  CPU: $(PROFILE_DIR)/bitmask_cpu.prof"
	@echo "  MEM: $(PROFILE_DIR)/bitmask_mem.prof"
	@echo ""
	@echo "$(YELLOW)View with:$(NC)"
	@echo "  make profile-view-cpu-bitmask"
	@echo "  make profile-view-mem-bitmask"
	@echo ""

## ═══════════════════════════════════════════════════════════
## Bulk Operations
## ═══════════════════════════════════════════════════════════

## profile-all: Profile all strategies sequentially
profile-all:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║        Profiling All 7 Strategies                          ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)This will take several minutes...$(NC)"
	@echo ""
	@$(MAKE) profile-channel
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) profile-work-stealing
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) profile-mpmc
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) profile-lmax
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) profile-priority-queue
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) profile-skip-list
	@echo "$(BLUE)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@$(MAKE) profile-bitmask
	@echo ""
	@echo "$(GREEN)╔════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(GREEN)║  ✅ All 7 strategies profiled successfully!                ║$(NC)"
	@echo "$(GREEN)╚════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(YELLOW)Profiles saved in $(PROFILE_DIR)/ directory:$(NC)"
	@ls -lh $(PROFILE_DIR)/*.prof 2>/dev/null || echo "  (no profiles found)"
	@echo ""

## ═══════════════════════════════════════════════════════════
## View CPU Profiles
## ═══════════════════════════════════════════════════════════

## profile-view-cpu-channel: View Channel CPU profile in browser
profile-view-cpu-channel:
	@if [ ! -f "$(PROFILE_DIR)/channel_cpu.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-channel$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Channel CPU profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/channel_cpu.prof

## profile-view-cpu-work-stealing: View Work-Stealing CPU profile in browser
profile-view-cpu-work-stealing:
	@if [ ! -f "$(PROFILE_DIR)/work-stealing_cpu.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-work-stealing$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Work-Stealing CPU profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/work-stealing_cpu.prof

## profile-view-cpu-mpmc: View MPMC Queue CPU profile in browser
profile-view-cpu-mpmc:
	@if [ ! -f "$(PROFILE_DIR)/mpmc_cpu.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-mpmc$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening MPMC Queue CPU profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/mpmc_cpu.prof

## profile-view-cpu-lmax: View LMAX Disruptor CPU profile in browser
profile-view-cpu-lmax:
	@if [ ! -f "$(PROFILE_DIR)/lmax_cpu.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-lmax$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening LMAX Disruptor CPU profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/lmax_cpu.prof

## profile-view-cpu-priority-queue: View Priority Queue CPU profile in browser
profile-view-cpu-priority-queue:
	@if [ ! -f "$(PROFILE_DIR)/priority-queue_cpu.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-priority-queue$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Priority Queue CPU profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/priority-queue_cpu.prof

## profile-view-cpu-skip-list: View Skip List CPU profile in browser
profile-view-cpu-skip-list:
	@if [ ! -f "$(PROFILE_DIR)/skip-list_cpu.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-skip-list$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Skip List CPU profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/skip-list_cpu.prof

## profile-view-cpu-bitmask: View Bitmask CPU profile in browser
profile-view-cpu-bitmask:
	@if [ ! -f "$(PROFILE_DIR)/bitmask_cpu.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-bitmask$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Bitmask CPU profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/bitmask_cpu.prof

## ═══════════════════════════════════════════════════════════
## View Memory Profiles
## ═══════════════════════════════════════════════════════════

## profile-view-mem-channel: View Channel memory profile in browser
profile-view-mem-channel:
	@if [ ! -f "$(PROFILE_DIR)/channel_mem.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-channel$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Channel memory profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/channel_mem.prof

## profile-view-mem-work-stealing: View Work-Stealing memory profile in browser
profile-view-mem-work-stealing:
	@if [ ! -f "$(PROFILE_DIR)/work-stealing_mem.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-work-stealing$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Work-Stealing memory profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/work-stealing_mem.prof

## profile-view-mem-mpmc: View MPMC Queue memory profile in browser
profile-view-mem-mpmc:
	@if [ ! -f "$(PROFILE_DIR)/mpmc_mem.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-mpmc$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening MPMC Queue memory profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/mpmc_mem.prof

## profile-view-mem-lmax: View LMAX Disruptor memory profile in browser
profile-view-mem-lmax:
	@if [ ! -f "$(PROFILE_DIR)/lmax_mem.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-lmax$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening LMAX Disruptor memory profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/lmax_mem.prof

## profile-view-mem-priority-queue: View Priority Queue memory profile in browser
profile-view-mem-priority-queue:
	@if [ ! -f "$(PROFILE_DIR)/priority-queue_mem.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-priority-queue$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Priority Queue memory profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/priority-queue_mem.prof

## profile-view-mem-skip-list: View Skip List memory profile in browser
profile-view-mem-skip-list:
	@if [ ! -f "$(PROFILE_DIR)/skip-list_mem.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-skip-list$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Skip List memory profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/skip-list_mem.prof

## profile-view-mem-bitmask: View Bitmask memory profile in browser
profile-view-mem-bitmask:
	@if [ ! -f "$(PROFILE_DIR)/bitmask_mem.prof" ]; then \
		echo "$(RED)Error: Profile file not found$(NC)"; \
		echo "$(YELLOW)Run this first: make profile-bitmask$(NC)"; \
		exit 1; \
	fi
	@echo "$(BLUE)Opening Bitmask memory profile in browser...$(NC)"
	@echo "$(YELLOW)URL: http://localhost:$(PROFILE_PORT)$(NC)"
	$(GO) tool pprof -http=:$(PROFILE_PORT) $(PROFILE_DIR)/bitmask_mem.prof

## ═══════════════════════════════════════════════════════════
## Utilities
## ═══════════════════════════════════════════════════════════

## profile-clean: Clean all profile files
profile-clean:
	@echo "$(BLUE)Cleaning profile files...$(NC)"
	@rm -rf $(PROFILE_DIR)
	@echo "$(GREEN)✅ Profile files cleaned!$(NC)"
