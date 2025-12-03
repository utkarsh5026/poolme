#!/bin/bash
set -e

echo "Testing PoolMe Benchmark Visualization"
echo "====================================="
echo ""

# Check if Python is installed
if ! command -v python3 &> /dev/null; then
    echo "ERROR: Python 3 is not installed"
    echo "Please install Python 3.11 or later"
    exit 1
fi

echo "✓ Python is installed ($(python3 --version))"
echo ""

# Check if requirements are installed
echo "Checking Python dependencies..."
if ! python3 -c "import matplotlib, plotly" &> /dev/null; then
    echo "Installing Python dependencies..."
    pip3 install -r requirements.txt
fi

echo "✓ Dependencies are installed"
echo ""

echo "Running visualization test with sample data..."
python3 visualize_benchmarks.py sample_benchmark.txt --output-dir test_results

echo ""
echo "✅ SUCCESS! Visualizations created successfully!"
echo ""
echo "Check the test_results/ directory for:"
echo "  - *.png files (static charts)"
echo "  - dashboard.html (interactive dashboard)"
echo "  - performance_heatmap.html (heatmap)"
echo "  - BENCHMARK_REPORT.md (summary report)"
echo ""
echo "Open test_results/dashboard.html in your browser!"
