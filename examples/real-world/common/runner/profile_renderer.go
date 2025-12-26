package runner

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
)

// renderProfileAnalysis displays the complete profile analysis results
func renderProfileAnalysis(result *ProfileAnalysisResult) {
	if result == nil || len(result.Profiles) == 0 {
		colorPrintf(Red, "No profile data to display\n")
		return
	}

	sortedProfiles := make([]StrategyProfile, len(result.Profiles))
	copy(sortedProfiles, result.Profiles)
	sort.Slice(sortedProfiles, func(i, j int) bool {
		if sortedProfiles[i].StrategyName == result.BaselineStrategy {
			return true
		}
		if sortedProfiles[j].StrategyName == result.BaselineStrategy {
			return false
		}
		return sortedProfiles[i].OverheadPct < sortedProfiles[j].OverheadPct
	})

	for i, profile := range sortedProfiles {
		if i > 0 {
			fmt.Println()
		}
		renderStrategyTopFunctions(profile)
	}

	// Render overhead comparison table LAST as conclusion
	fmt.Println()
	renderOverheadComparison(sortedProfiles, result.BaselineStrategy)
}

// renderOverheadComparison renders the overhead comparison table
func renderOverheadComparison(profiles []StrategyProfile, baselineStrategy string) {
	colorPrintLn(Bold, "═══════════════════════════════════════════════════════════")
	colorPrintLn(Bold, "🎯 CPU OVERHEAD SUMMARY (vs Channel Baseline)")
	colorPrintLn(Bold, "═══════════════════════════════════════════════════════════")
	colorPrintLn(Yellow, "Lower CPU time = Better performance")
	fmt.Println()

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Strategy", "Total CPU Time", "Performance")

	for _, profile := range profiles {
		strategyName := profile.StrategyName
		totalTime := formatProfileDuration(profile.TotalSamples)

		var perfStr string
		if profile.StrategyName == baselineStrategy {
			perfStr = "Baseline (0%)"
		} else {
			if profile.OverheadPct < 0 {
				perfStr = fmt.Sprintf("%.2f%% faster ⚡", -profile.OverheadPct)
			} else {
				perfStr = fmt.Sprintf("%.2f%% slower", profile.OverheadPct)
			}
		}

		_ = table.Append(strategyName, totalTime, perfStr)
	}

	_ = table.Render()
}

// renderStrategyTopFunctions renders the top functions table for a single strategy
func renderStrategyTopFunctions(profile StrategyProfile) {
	colorPrintLn(Bold, "═══════════════════════════════════════════════════════════")
	colorPrintf(Bold, "📊 %s - Top %d Hottest Functions\n", profile.StrategyName, len(profile.TopFunctions))
	colorPrintLn(Bold, "═══════════════════════════════════════════════════════════")
	fmt.Println()

	if len(profile.TopFunctions) == 0 {
		colorPrintf(Yellow, "No function data available for %s\n", profile.StrategyName)
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Function", "Flat", "Flat%", "Cum", "Cum%")

	for rank, fn := range profile.TopFunctions {
		rankStr := fmt.Sprintf("%d", rank+1)
		flatStr := formatProfileDuration(fn.FlatTime)
		flatPctStr := fmt.Sprintf("%.2f%%", fn.FlatPercent)
		cumStr := formatProfileDuration(fn.CumTime)
		cumPctStr := fmt.Sprintf("%.2f%%", fn.CumPercent)
		funcName := formatFunctionName(fn.FunctionName)

		_ = table.Append(rankStr, funcName, flatStr, flatPctStr, cumStr, cumPctStr)
	}

	_ = table.Render()
}

// formatProfileDuration formats a duration for profile display
func formatProfileDuration(d time.Duration) string {
	if d >= time.Second {
		seconds := float64(d) / float64(time.Second)
		return fmt.Sprintf("%.2fs", seconds)
	}

	if d >= time.Millisecond {
		ms := float64(d) / float64(time.Millisecond)
		return fmt.Sprintf("%.0fms", ms)
	}

	if d >= time.Microsecond {
		us := float64(d) / float64(time.Microsecond)
		return fmt.Sprintf("%.0fµs", us)
	}

	return fmt.Sprintf("%dns", d.Nanoseconds())
}

// formatFunctionName makes function names more readable by extracting the relevant parts
func formatFunctionName(fullName string) string {
	if len(fullName) <= 40 {
		return fullName
	}

	// Try to extract just the package and function name
	// Example: "github.com/user/repo/pkg.FunctionName" -> "pkg.FunctionName"
	parts := strings.Split(fullName, "/")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		if strings.Contains(lastPart, ".") {
			return lastPart
		}
	}

	if len(fullName) > 50 {
		return fullName[:47] + "..."
	}

	return fullName
}
