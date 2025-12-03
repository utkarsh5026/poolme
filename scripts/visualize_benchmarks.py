#!/usr/bin/env python3
"""
Benchmark Visualization Script for PoolMe

This script parses Go benchmark output and creates interactive visualizations
using Plotly and static charts using Matplotlib.

Usage:
    python visualize_benchmarks.py <benchmark_output.txt>
    python visualize_benchmarks.py --run-benchmarks  # Run benchmarks first, then visualize
"""

import re
import sys
import subprocess
import argparse
from pathlib import Path
from typing import Dict, List, Tuple, Optional
from dataclasses import dataclass
import json

try:
    import matplotlib.pyplot as plt
    import matplotlib

    matplotlib.use("Agg")  # Use non-interactive backend
except ImportError:
    print("matplotlib not found. Install with: pip install matplotlib")
    sys.exit(1)

try:
    import plotly.graph_objects as go
    import plotly.express as px
    from plotly.subplots import make_subplots
except ImportError:
    print("plotly not found. Install with: pip install plotly")
    sys.exit(1)


@dataclass
class BenchmarkResult:
    """Represents a single benchmark result"""

    name: str
    strategy: str
    benchmark_type: str
    ns_per_op: float
    allocs_per_op: int = 0
    bytes_per_op: int = 0
    custom_metrics: Optional[Dict[str, float]] = None

    def __post_init__(self):
        if self.custom_metrics is None:
            self.custom_metrics = {}


class BenchmarkParser:
    """Parses Go benchmark output"""

    BENCHMARK_PATTERN = re.compile(
        r"Benchmark([^/\s]+)/([^-\s]+)-\d+\s+"  # BenchmarkName/Strategy-cores
        r"(\d+)\s+"  # iterations
        r"([\d.]+)\s+ns/op"  # ns/op
        r"(?:\s+([\d.]+)\s+([^/\s]+)/([^\s]+))?"  # custom metric (e.g., 1234 tasks/sec)
    )

    ALLOC_PATTERN = re.compile(r"(\d+)\s+B/op\s+(\d+)\s+allocs/op")

    def parse_file(self, filepath: str) -> List[BenchmarkResult]:
        """Parse benchmark output file"""
        results = []

        with open(filepath, "r") as f:
            lines = f.readlines()

        for i, line in enumerate(lines):
            if line.startswith("Benchmark"):
                result = self._parse_line(line, lines, i)
                if result:
                    results.append(result)

        return results

    def _parse_line(
        self, line: str, all_lines: List[str], index: int
    ) -> Optional[BenchmarkResult]:
        """Parse a single benchmark line"""
        match = self.BENCHMARK_PATTERN.search(line)
        if not match:
            return None

        benchmark_type = match.group(1)
        strategy = match.group(2)
        ns_per_op = float(match.group(4))

        custom_metrics = {}

        # Extract custom metrics (e.g., tasks/sec, p99_ns, etc.)
        metric_pattern = re.compile(r"([\d.]+)\s+([^\s]+)")
        parts = line.split("\t")
        for part in parts[1:]:
            m = metric_pattern.match(part.strip())
            if m and m.group(2) not in ["ns/op", "B/op", "allocs/op"]:
                metric_name = m.group(2)
                metric_value = float(m.group(1))
                custom_metrics[metric_name] = metric_value

        # Check for allocation info on the same line or next line
        allocs_per_op = 0
        bytes_per_op = 0
        alloc_match = self.ALLOC_PATTERN.search(line)
        if alloc_match:
            bytes_per_op = int(alloc_match.group(1))
            allocs_per_op = int(alloc_match.group(2))
        elif index + 1 < len(all_lines):
            alloc_match = self.ALLOC_PATTERN.search(all_lines[index + 1])
            if alloc_match:
                bytes_per_op = int(alloc_match.group(1))
                allocs_per_op = int(alloc_match.group(2))

        return BenchmarkResult(
            name=f"{benchmark_type}/{strategy}",
            strategy=strategy,
            benchmark_type=benchmark_type,
            ns_per_op=ns_per_op,
            allocs_per_op=allocs_per_op,
            bytes_per_op=bytes_per_op,
            custom_metrics=custom_metrics,
        )


class BenchmarkVisualizer:
    """Creates visualizations from benchmark results"""

    def __init__(
        self, results: List[BenchmarkResult], output_dir: str = "benchmark_results"
    ):
        self.results = results
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(exist_ok=True)

        # Group results by benchmark type
        self.grouped_results = self._group_by_benchmark_type()

    def _group_by_benchmark_type(self) -> Dict[str, List[BenchmarkResult]]:
        """Group results by benchmark type"""
        grouped = {}
        for result in self.results:
            if result.benchmark_type not in grouped:
                grouped[result.benchmark_type] = []
            grouped[result.benchmark_type].append(result)
        return grouped

    def create_all_visualizations(self):
        """Create all visualizations"""
        print(f"Creating visualizations in {self.output_dir}...")

        # Performance comparison
        self._create_performance_comparison()

        # Memory allocation comparison
        self._create_memory_comparison()

        # Strategy comparison by workload type
        self._create_workload_comparison()

        # Custom metrics visualizations
        self._create_custom_metrics_viz()

        # Interactive HTML dashboard
        self._create_interactive_dashboard()

        print(f"✓ Visualizations created in {self.output_dir}/")

    def _create_performance_comparison(self):
        """Create performance comparison chart (ns/op)"""
        # Group by benchmark type for better comparison
        for bench_type, results in self.grouped_results.items():
            if not results:
                continue

            strategies = [r.strategy for r in results]
            ns_per_op = [r.ns_per_op for r in results]

            # Create matplotlib chart
            fig, ax = plt.subplots(figsize=(12, 6))
            bars = ax.bar(
                strategies, ns_per_op, color="skyblue", edgecolor="navy", alpha=0.7
            )

            # Add value labels on bars
            for bar in bars:
                height = bar.get_height()
                ax.text(
                    bar.get_x() + bar.get_width() / 2.0,
                    height,
                    f"{int(height):,}",
                    ha="center",
                    va="bottom",
                    fontsize=9,
                )

            ax.set_xlabel("Strategy", fontsize=12, fontweight="bold")
            ax.set_ylabel("Time (ns/op)", fontsize=12, fontweight="bold")
            ax.set_title(
                f"{bench_type} - Performance Comparison (Lower is Better)",
                fontsize=14,
                fontweight="bold",
            )
            ax.grid(axis="y", alpha=0.3)
            plt.xticks(rotation=45, ha="right")
            plt.tight_layout()

            output_file = self.output_dir / f"{bench_type}_performance.png"
            plt.savefig(output_file, dpi=300, bbox_inches="tight")
            plt.close()
            print(f"  ✓ Created {output_file}")

    def _create_memory_comparison(self):
        """Create memory allocation comparison"""
        results_with_allocs = [r for r in self.results if r.allocs_per_op > 0]

        if not results_with_allocs:
            return

        # Group by benchmark type
        for bench_type, results in self.grouped_results.items():
            results = [r for r in results if r.allocs_per_op > 0]
            if not results:
                continue

            strategies = [r.strategy for r in results]
            allocs = [r.allocs_per_op for r in results]
            bytes_alloc = [r.bytes_per_op for r in results]

            # Create subplot with two charts
            fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(16, 6))

            # Allocations per op
            bars1 = ax1.bar(
                strategies, allocs, color="coral", edgecolor="darkred", alpha=0.7
            )
            for bar in bars1:
                height = bar.get_height()
                ax1.text(
                    bar.get_x() + bar.get_width() / 2.0,
                    height,
                    f"{int(height):,}",
                    ha="center",
                    va="bottom",
                    fontsize=9,
                )

            ax1.set_xlabel("Strategy", fontsize=12, fontweight="bold")
            ax1.set_ylabel("Allocations per Op", fontsize=12, fontweight="bold")
            ax1.set_title(
                "Memory Allocations (Lower is Better)", fontsize=13, fontweight="bold"
            )
            ax1.grid(axis="y", alpha=0.3)
            ax1.tick_params(axis="x", rotation=45)

            # Bytes per op
            bars2 = ax2.bar(
                strategies,
                bytes_alloc,
                color="lightgreen",
                edgecolor="darkgreen",
                alpha=0.7,
            )
            for bar in bars2:
                height = bar.get_height()
                ax2.text(
                    bar.get_x() + bar.get_width() / 2.0,
                    height,
                    f"{int(height):,}",
                    ha="center",
                    va="bottom",
                    fontsize=9,
                )

            ax2.set_xlabel("Strategy", fontsize=12, fontweight="bold")
            ax2.set_ylabel("Bytes per Op", fontsize=12, fontweight="bold")
            ax2.set_title(
                "Memory Usage (Lower is Better)", fontsize=13, fontweight="bold"
            )
            ax2.grid(axis="y", alpha=0.3)
            ax2.tick_params(axis="x", rotation=45)

            plt.suptitle(
                f"{bench_type} - Memory Analysis",
                fontsize=15,
                fontweight="bold",
                y=1.02,
            )
            plt.tight_layout()

            output_file = self.output_dir / f"{bench_type}_memory.png"
            plt.savefig(output_file, dpi=300, bbox_inches="tight")
            plt.close()
            print(f"  ✓ Created {output_file}")

    def _create_workload_comparison(self):
        """Create comparison across different workload types"""
        # Get all unique strategies
        all_strategies = set(r.strategy for r in self.results)

        # Create a comparison chart showing how each strategy performs across workloads
        fig, ax = plt.subplots(figsize=(14, 8))

        x = list(self.grouped_results.keys())
        x_pos = range(len(x))
        bar_width = 0.8 / len(all_strategies)

        for i, strategy in enumerate(sorted(all_strategies)):
            strategy_times = []
            for bench_type in x:
                results = [
                    r
                    for r in self.grouped_results[bench_type]
                    if r.strategy == strategy
                ]
                avg_time = (
                    sum(r.ns_per_op for r in results) / len(results) if results else 0
                )
                strategy_times.append(avg_time)

            offset = (i - len(all_strategies) / 2) * bar_width
            positions = [p + offset for p in x_pos]
            ax.bar(positions, strategy_times, bar_width, label=strategy, alpha=0.8)

        ax.set_xlabel("Benchmark Type", fontsize=12, fontweight="bold")
        ax.set_ylabel("Time (ns/op)", fontsize=12, fontweight="bold")
        ax.set_title(
            "Strategy Performance Across Workload Types", fontsize=14, fontweight="bold"
        )
        ax.set_xticks(x_pos)
        ax.set_xticklabels(x, rotation=45, ha="right")
        ax.legend(loc="upper left", bbox_to_anchor=(1, 1))
        ax.grid(axis="y", alpha=0.3)

        plt.tight_layout()
        output_file = self.output_dir / "workload_comparison.png"
        plt.savefig(output_file, dpi=300, bbox_inches="tight")
        plt.close()
        print(f"  ✓ Created {output_file}")

    def _create_custom_metrics_viz(self):
        """Create visualizations for custom metrics like tasks/sec, latency percentiles"""
        # Collect all custom metrics
        all_metrics = set()
        for result in self.results:
            all_metrics.update(result.custom_metrics.keys())

        for metric in all_metrics:
            # Group by benchmark type
            for bench_type, results in self.grouped_results.items():
                results_with_metric = [r for r in results if metric in r.custom_metrics]
                if not results_with_metric:
                    continue

                strategies = [r.strategy for r in results_with_metric]
                values = [r.custom_metrics[metric] for r in results_with_metric]

                fig, ax = plt.subplots(figsize=(12, 6))

                # Use different colors for different metric types
                color = "lightblue"
                if "tasks/sec" in metric:
                    color = "lightgreen"
                elif "latency" in metric or "p" in metric or "ns" in metric:
                    color = "coral"

                bars = ax.bar(
                    strategies, values, color=color, edgecolor="navy", alpha=0.7
                )

                for bar in bars:
                    height = bar.get_height()
                    ax.text(
                        bar.get_x() + bar.get_width() / 2.0,
                        height,
                        f"{height:.2f}",
                        ha="center",
                        va="bottom",
                        fontsize=9,
                    )

                ax.set_xlabel("Strategy", fontsize=12, fontweight="bold")
                ax.set_ylabel(metric, fontsize=12, fontweight="bold")

                # Determine if higher or lower is better
                better = "Higher is Better"
                if "latency" in metric or "ns" in metric or metric.startswith("p"):
                    better = "Lower is Better"

                ax.set_title(
                    f"{bench_type} - {metric} ({better})",
                    fontsize=14,
                    fontweight="bold",
                )
                ax.grid(axis="y", alpha=0.3)
                plt.xticks(rotation=45, ha="right")
                plt.tight_layout()

                safe_metric_name = metric.replace("/", "_").replace(" ", "_")
                output_file = self.output_dir / f"{bench_type}_{safe_metric_name}.png"
                plt.savefig(output_file, dpi=300, bbox_inches="tight")
                plt.close()
                print(f"  ✓ Created {output_file}")

    def _create_interactive_dashboard(self):
        """Create an interactive HTML dashboard using Plotly"""
        # Create subplots
        fig = make_subplots(
            rows=2,
            cols=2,
            subplot_titles=(
                "Performance Comparison",
                "Throughput Comparison",
                "Memory Allocations",
                "Latency Percentiles",
            ),
            specs=[
                [{"type": "bar"}, {"type": "bar"}],
                [{"type": "bar"}, {"type": "bar"}],
            ],
        )

        # Get data for different visualizations
        for bench_type, results in list(self.grouped_results.items())[
            :1
        ]:  # Use first benchmark
            strategies = [r.strategy for r in results]
            ns_per_op = [r.ns_per_op for r in results]

            # Performance (ns/op)
            fig.add_trace(
                go.Bar(
                    x=strategies,
                    y=ns_per_op,
                    name="ns/op",
                    marker_color="skyblue",
                    showlegend=False,
                ),
                row=1,
                col=1,
            )

            # Throughput (if available)
            throughput_results = [r for r in results if "tasks/sec" in r.custom_metrics]
            if throughput_results:
                strategies_tp = [r.strategy for r in throughput_results]
                throughput = [r.custom_metrics["tasks/sec"] for r in throughput_results]
                fig.add_trace(
                    go.Bar(
                        x=strategies_tp,
                        y=throughput,
                        name="tasks/sec",
                        marker_color="lightgreen",
                        showlegend=False,
                    ),
                    row=1,
                    col=2,
                )

            # Memory allocations
            alloc_results = [r for r in results if r.allocs_per_op > 0]
            if alloc_results:
                strategies_mem = [r.strategy for r in alloc_results]
                allocs = [r.allocs_per_op for r in alloc_results]
                fig.add_trace(
                    go.Bar(
                        x=strategies_mem,
                        y=allocs,
                        name="allocs/op",
                        marker_color="coral",
                        showlegend=False,
                    ),
                    row=2,
                    col=1,
                )

            # Latency percentiles
            latency_results = [r for r in results if "p99_ns" in r.custom_metrics]
            if latency_results:
                strategies_lat = [r.strategy for r in latency_results]
                p99 = [r.custom_metrics["p99_ns"] for r in latency_results]
                fig.add_trace(
                    go.Bar(
                        x=strategies_lat,
                        y=p99,
                        name="p99 latency",
                        marker_color="mediumpurple",
                        showlegend=False,
                    ),
                    row=2,
                    col=2,
                )

        # Update layout
        fig.update_layout(
            title_text="PoolMe Benchmark Dashboard",
            title_font_size=24,
            showlegend=False,
            height=800,
            template="plotly_white",
        )

        # Update axes
        fig.update_xaxes(tickangle=45)
        fig.update_yaxes(title_text="ns/op", row=1, col=1)
        fig.update_yaxes(title_text="tasks/sec", row=1, col=2)
        fig.update_yaxes(title_text="allocs/op", row=2, col=1)
        fig.update_yaxes(title_text="p99 (ns)", row=2, col=2)

        # Save as HTML
        output_file = self.output_dir / "dashboard.html"
        fig.write_html(str(output_file))
        print(f"  ✓ Created interactive dashboard: {output_file}")

        # Also create a detailed comparison chart
        self._create_detailed_interactive_comparison()

    def _create_detailed_interactive_comparison(self):
        """Create a detailed interactive comparison across all benchmarks"""
        strategies = sorted(set(r.strategy for r in self.results))
        bench_types = sorted(self.grouped_results.keys())

        # Create matrix of performance (normalized)
        matrix = []
        for strategy in strategies:
            row = []
            for bench_type in bench_types:
                results = [
                    r
                    for r in self.grouped_results[bench_type]
                    if r.strategy == strategy
                ]
                avg_time = (
                    sum(r.ns_per_op for r in results) / len(results) if results else 0
                )
                row.append(avg_time)
            matrix.append(row)

        fig = go.Figure(
            data=go.Heatmap(
                z=matrix,
                x=bench_types,
                y=strategies,
                colorscale="RdYlGn_r",
                text=[[f"{val:,.0f}" for val in row] for row in matrix],
                texttemplate="%{text}",
                textfont={"size": 10},
                colorbar=dict(title="ns/op"),
            )
        )

        fig.update_layout(
            title="Strategy Performance Heatmap (Lower is Better)",
            xaxis_title="Benchmark Type",
            yaxis_title="Strategy",
            height=600,
            template="plotly_white",
        )

        fig.update_xaxes(tickangle=45)

        output_file = self.output_dir / "performance_heatmap.html"
        fig.write_html(str(output_file))
        print(f"  ✓ Created performance heatmap: {output_file}")

    def generate_summary_report(self):
        """Generate a markdown summary report"""
        report_lines = [
            "# PoolMe Benchmark Results\n",
            f"Total benchmarks: {len(self.results)}\n",
            f"Strategies tested: {len(set(r.strategy for r in self.results))}\n",
            f"Benchmark types: {len(self.grouped_results)}\n\n",
            "## Performance Summary\n\n",
        ]

        for bench_type, results in sorted(self.grouped_results.items()):
            report_lines.append(f"### {bench_type}\n\n")
            report_lines.append("| Strategy | ns/op | Allocs/op | Bytes/op |\n")
            report_lines.append("|----------|-------|-----------|----------|\n")

            for result in sorted(results, key=lambda r: r.ns_per_op):
                report_lines.append(
                    f"| {result.strategy} | {result.ns_per_op:,.2f} | "
                    f"{result.allocs_per_op:,} | {result.bytes_per_op:,} |\n"
                )

            # Add custom metrics if available
            if results and results[0].custom_metrics:
                report_lines.append("\n**Custom Metrics:**\n\n")
                for metric in results[0].custom_metrics.keys():
                    report_lines.append(f"| Strategy | {metric} |\n")
                    report_lines.append(
                        "|----------|" + "-" * (len(metric) + 2) + "|\n"
                    )
                    for result in results:
                        if metric in result.custom_metrics:
                            report_lines.append(
                                f"| {result.strategy} | {result.custom_metrics[metric]:.2f} |\n"
                            )
                    report_lines.append("\n")

            report_lines.append("\n")

        # Find best performers
        report_lines.append("## Best Performers\n\n")
        for bench_type, results in sorted(self.grouped_results.items()):
            best = min(results, key=lambda r: r.ns_per_op)
            report_lines.append(
                f"- **{bench_type}**: {best.strategy} ({best.ns_per_op:,.2f} ns/op)\n"
            )

        output_file = self.output_dir / "BENCHMARK_REPORT.md"
        with open(output_file, "w") as f:
            f.writelines(report_lines)

        print(f"  ✓ Created summary report: {output_file}")


def run_benchmarks() -> str:
    """Run Go benchmarks and return output file path"""
    print("Running benchmarks...")
    output_file = Path("benchmark_results") / "benchmark_output.txt"
    output_file.parent.mkdir(exist_ok=True)

    try:
        result = subprocess.run(
            ["go", "test", "-bench=.", "-benchmem", "-benchtime=100x", "./benchmarks"],
            capture_output=True,
            text=True,
            check=True,
        )

        with open(output_file, "w") as f:
            f.write(result.stdout)

        print(f"✓ Benchmarks complete, output saved to {output_file}")
        return str(output_file)

    except subprocess.CalledProcessError as e:
        print(f"Error running benchmarks: {e}")
        print(e.stderr)
        sys.exit(1)


def main():
    parser = argparse.ArgumentParser(description="Visualize PoolMe benchmark results")
    parser.add_argument("input_file", nargs="?", help="Benchmark output file to parse")
    parser.add_argument(
        "--run-benchmarks",
        action="store_true",
        help="Run benchmarks first, then visualize",
    )
    parser.add_argument(
        "--output-dir",
        default="benchmark_results",
        help="Output directory for visualizations",
    )

    args = parser.parse_args()

    if args.run_benchmarks:
        input_file = run_benchmarks()
    elif args.input_file:
        input_file = args.input_file
    else:
        print("Error: Please provide an input file or use --run-benchmarks")
        parser.print_help()
        sys.exit(1)

    print(f"\nParsing benchmark results from {input_file}...")
    parser_obj = BenchmarkParser()
    results = parser_obj.parse_file(input_file)

    if not results:
        print("No benchmark results found in the input file")
        sys.exit(1)

    print(f"✓ Parsed {len(results)} benchmark results")

    # Create visualizations
    visualizer = BenchmarkVisualizer(results, args.output_dir)
    visualizer.create_all_visualizations()
    visualizer.generate_summary_report()

    print(f"\n✅ All done! Check {args.output_dir}/ for visualizations")
    print(
        f"   - Open {args.output_dir}/dashboard.html in your browser for interactive dashboard"
    )


if __name__ == "__main__":
    main()
