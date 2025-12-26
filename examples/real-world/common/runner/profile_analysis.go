package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ProfileFunctionStat represents statistics for a single function in the profile
type ProfileFunctionStat struct {
	FunctionName string
	FlatTime     time.Duration
	FlatPercent  float64
	CumTime      time.Duration
	CumPercent   float64
}

// StrategyProfile holds profiling analysis for a single strategy
type StrategyProfile struct {
	StrategyName string
	TotalSamples time.Duration
	TopFunctions []ProfileFunctionStat
	OverheadPct  float64 // Percentage overhead relative to baseline
}

// ProfileAnalysisResult holds the complete profile analysis results
type ProfileAnalysisResult struct {
	BaselineStrategy string
	BaselineTime     time.Duration
	Profiles         []StrategyProfile
}

// StrategyNameToFilename converts a strategy display name to its profile filename format
func StrategyNameToFilename(strategy string) string {
	switch strategy {
	case "Work-Stealing":
		return "work-stealing"
	case "LMAX Disruptor":
		return "lmax"
	case "MPMC Queue":
		return "mpmc"
	case "Priority Queue":
		return "priority-queue"
	case "Skip List":
		return "skip-list"
	case "Channel":
		return "channel"
	case "Bitmask":
		return "bitmask"
	default:
		return strings.ToLower(strings.ReplaceAll(strategy, " ", "-"))
	}
}

// ParseProfileFile parses a pprof file and extracts top N functions
func ParseProfileFile(profilePath string, topN int) ([]ProfileFunctionStat, time.Duration, error) {
	// Check if file exists
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		return nil, 0, fmt.Errorf("profile file not found: %s", profilePath)
	}

	// Run go tool pprof to get text output
	cmd := exec.Command("go", "tool", "pprof", "-text", "-nodecount="+strconv.Itoa(topN), profilePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, 0, fmt.Errorf("pprof command failed: %w\nOutput: %s", err, string(output))
	}

	return parsePprofTextOutput(string(output), topN)
}

// parsePprofTextOutput parses the text output from go tool pprof
func parsePprofTextOutput(output string, topN int) ([]ProfileFunctionStat, time.Duration, error) {
	lines := strings.Split(output, "\n")

	var totalDuration time.Duration
	var functions []ProfileFunctionStat

	// Regex patterns for parsing
	// Sample line: "     2.58s 43.23% 43.23%      5.96s   100%  main.processTask"
	funcPattern := regexp.MustCompile(`^\s*(\d+\.?\d*[a-z]*)\s+(\d+\.?\d+)%\s+\d+\.?\d+%\s+(\d+\.?\d*[a-z]*)\s+(\d+\.?\d+)%\s+(.+)$`)

	// Extract total duration from header (e.g., "Duration: 5.96s, Total samples = 5.96s")
	durationPattern := regexp.MustCompile(`(?:Duration|Total samples)\s*[=:]\s*(\d+\.?\d*[a-z]+)`)

	for _, line := range lines {
		// Try to extract total duration
		if matches := durationPattern.FindStringSubmatch(line); len(matches) > 1 {
			if dur, err := parseDuration(matches[1]); err == nil {
				totalDuration = dur
			}
		}

		// Parse function statistics
		if matches := funcPattern.FindStringSubmatch(line); len(matches) == 6 {
			flatTime, err1 := parseDuration(matches[1])
			flatPercent, err2 := strconv.ParseFloat(matches[2], 64)
			cumTime, err3 := parseDuration(matches[3])
			cumPercent, err4 := strconv.ParseFloat(matches[4], 64)
			funcName := strings.TrimSpace(matches[5])

			if err1 == nil && err2 == nil && err3 == nil && err4 == nil {
				functions = append(functions, ProfileFunctionStat{
					FunctionName: funcName,
					FlatTime:     flatTime,
					FlatPercent:  flatPercent,
					CumTime:      cumTime,
					CumPercent:   cumPercent,
				})
			}
		}
	}

	// If we didn't find total duration from header, estimate from cumulative time of first function
	if totalDuration == 0 && len(functions) > 0 {
		totalDuration = functions[0].CumTime
	}

	// Limit to topN functions
	if len(functions) > topN {
		functions = functions[:topN]
	}

	if len(functions) == 0 {
		return nil, totalDuration, fmt.Errorf("no function statistics found in profile output")
	}

	return functions, totalDuration, nil
}

// parseDuration parses duration strings like "2.58s", "890ms", "100ns"
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	// Handle common formats
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ms") && !strings.HasSuffix(s, "ns") {
		// Seconds
		val, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(val * float64(time.Second)), nil
	}

	if strings.HasSuffix(s, "ms") {
		val, err := strconv.ParseFloat(strings.TrimSuffix(s, "ms"), 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(val * float64(time.Millisecond)), nil
	}

	if strings.HasSuffix(s, "ns") {
		val, err := strconv.ParseFloat(strings.TrimSuffix(s, "ns"), 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(val), nil
	}

	if strings.HasSuffix(s, "us") || strings.HasSuffix(s, "µs") {
		val, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(s, "us"), "µs"), 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(val * float64(time.Microsecond)), nil
	}

	// Try standard time.ParseDuration as fallback
	return time.ParseDuration(s)
}

// AnalyzeStrategies analyzes CPU profiles for all strategies and calculates overhead
func AnalyzeStrategies(profileDir string, strategies []string, topN int) (*ProfileAnalysisResult, error) {
	result := &ProfileAnalysisResult{
		BaselineStrategy: "Channel",
		Profiles:         make([]StrategyProfile, 0, len(strategies)),
	}

	var baselineTime time.Duration
	var profilesFound int

	// First pass: parse all profiles
	for _, strategy := range strategies {
		filename := StrategyNameToFilename(strategy)
		profilePath := filepath.Join(profileDir, filename+"_cpu.prof")

		functions, totalTime, err := ParseProfileFile(profilePath, topN)
		if err != nil {
			// Warn but continue with other strategies
			colorPrintf(Yellow, "  ⚠️  Skipping %s: %v\n", strategy, err)
			continue
		}

		profilesFound++

		profile := StrategyProfile{
			StrategyName: strategy,
			TotalSamples: totalTime,
			TopFunctions: functions,
		}

		// Store baseline time
		if strategy == "Channel" {
			baselineTime = totalTime
			result.BaselineTime = totalTime
		}

		result.Profiles = append(result.Profiles, profile)
	}

	if profilesFound == 0 {
		return nil, fmt.Errorf("no valid profile files found in %s", profileDir)
	}

	// If Channel profile not found, use first strategy as baseline
	if baselineTime == 0 && len(result.Profiles) > 0 {
		baselineTime = result.Profiles[0].TotalSamples
		result.BaselineStrategy = result.Profiles[0].StrategyName
		result.BaselineTime = baselineTime
		colorPrintf(Yellow, "  ℹ️  Channel profile not found, using %s as baseline\n", result.BaselineStrategy)
	}

	// Second pass: calculate overhead percentages
	for i := range result.Profiles {
		if baselineTime > 0 {
			overheadNs := result.Profiles[i].TotalSamples - baselineTime
			result.Profiles[i].OverheadPct = (float64(overheadNs) / float64(baselineTime)) * 100.0
		}
	}

	return result, nil
}
