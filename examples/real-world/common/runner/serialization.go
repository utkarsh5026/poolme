package runner

import (
	"encoding/json"
	"fmt"
)

// JSONBenchmarkOutput wraps benchmark results for JSON output
type JSONBenchmarkOutput struct {
	BenchmarkType string           `json:"benchmark_type"`
	Results       []StrategyResult `json:"results"`
}

// PopulateStringFields populates human-readable string fields for JSON output
func PopulateStringFields(results []StrategyResult) {
	for i := range results {
		r := &results[i]
		r.TotalTimeStr = FormatLatency(r.TotalTime)
		r.AvgLatencyStr = FormatLatency(r.AvgLatency)
		r.P50LatencyStr = FormatLatency(r.P50Latency)
		r.P95LatencyStr = FormatLatency(r.P95Latency)
		r.P99LatencyStr = FormatLatency(r.P99Latency)
	}
}

// SerializeToJSON converts results to JSON bytes
func SerializeToJSON(benchType string, results []StrategyResult) ([]byte, error) {
	PopulateStringFields(results)

	output := JSONBenchmarkOutput{
		BenchmarkType: benchType,
		Results:       results,
	}

	return json.MarshalIndent(output, "", "  ")
}

// OutputJSON prints JSON to stdout
func OutputJSON(benchType string, results []StrategyResult) error {
	data, err := SerializeToJSON(benchType, results)
	if err != nil {
		return fmt.Errorf("failed to serialize to JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}
