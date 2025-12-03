@echo off
echo Testing PoolMe Benchmark Visualization
echo =====================================
echo.

REM Check if Python is installed
python --version >nul 2>&1
if %errorlevel% neq 0 (
    echo ERROR: Python is not installed or not in PATH
    echo Please install Python 3.11 or later from https://www.python.org/
    exit /b 1
)

echo ✓ Python is installed
echo.

REM Check if requirements are installed
echo Checking Python dependencies...
python -c "import matplotlib, plotly" >nul 2>&1
if %errorlevel% neq 0 (
    echo Installing Python dependencies...
    pip install -r requirements.txt
    if %errorlevel% neq 0 (
        echo ERROR: Failed to install dependencies
        exit /b 1
    )
)

echo ✓ Dependencies are installed
echo.

echo Running visualization test with sample data...
python visualize_benchmarks.py sample_benchmark.txt --output-dir test_results

if %errorlevel% eq 0 (
    echo.
    echo ✅ SUCCESS! Visualizations created successfully!
    echo.
    echo Check the test_results/ directory for:
    echo   - *.png files (static charts)
    echo   - dashboard.html (interactive dashboard)
    echo   - performance_heatmap.html (heatmap)
    echo   - BENCHMARK_REPORT.md (summary report)
    echo.
    echo Open test_results/dashboard.html in your browser!
) else (
    echo.
    echo ❌ ERROR: Visualization generation failed
    exit /b 1
)
