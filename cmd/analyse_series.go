package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
	dataexport "github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"
	printstyle "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/print-style"
)

// validateSeriesFlags rejects invalid --best-of/--demo combinations before
// any file is opened or parsed. bestOfSet is whether the user supplied the
// flag at all: only an unset flag keeps the existing single-demo flow (one
// path, or none for the picker), so an explicit --best-of 0 is rejected like
// any other value outside 3 and 5 instead of silently reading as absent. A
// series is never inferred from the file count; with the flag set,
// the analysis package owns the format rule and this only rewords it in flag terms.
func validateSeriesFlags(bestOf int, bestOfSet bool, demoCount int) error {
	if !bestOfSet {
		if demoCount > 1 {
			return fmt.Errorf("%d --demo values need an explicit series format: add --best-of 3 or --best-of 5", demoCount)
		}
		return nil
	}
	minDemos, maxDemos, err := analysis.SeriesMapCountRange(bestOf)
	if err != nil {
		return fmt.Errorf("invalid --best-of %d, must be 3 or 5", bestOf)
	}
	if demoCount < minDemos || demoCount > maxDemos {
		counts := "2 or 3"
		if bestOf == 5 {
			counts = "3, 4 or 5"
		}
		return fmt.Errorf("--best-of %d takes %s --demo values in played order, got %d", bestOf, counts, demoCount)
	}
	return nil
}

// hashDemoFiles streams every demo through SHA-256 — never loading a demo
// into memory — and rejects duplicate content before anything is parsed, so
// the same map supplied under two paths fails fast.
func hashDemoFiles(paths []string) ([]string, error) {
	digests := make([]string, len(paths))
	seen := make(map[string]int, len(paths))
	for i, path := range paths {
		digest, err := hashDemoFile(path)
		if err != nil {
			return nil, err
		}
		if first, duplicate := seen[digest]; duplicate {
			return nil, fmt.Errorf("--demo %s repeats the content of %s (SHA-256 %s)", path, paths[first], digest)
		}
		seen[digest] = i
		digests[i] = digest
	}
	return digests, nil
}

func hashDemoFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening demo file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// runSeriesAnalysis is the multi-demo path: hash, parse in command-line
// order, build the series, resolve the player selection against cross-map
// identity, then render and optionally save. It never opens the demo file
// picker or the interactive player selector; without --players every player
// is analysed.
func runSeriesAnalysis(ctx context.Context, bestOf int, paths []string) error {
	digests, err := hashDemoFiles(paths)
	if err != nil {
		return err
	}

	lipgloss.Println(printstyle.StyleInfo.Render("Processing CS2 series, hang tight... \n"))
	startTime := time.Now()
	inputs := make([]analysis.SeriesMapInput, len(paths))
	for i, path := range paths {
		fmt.Printf("Parsing map %d/%d: %s\n", i+1, len(paths), path)
		demo, err := analysis.AnalyseFile(ctx, path)
		if err != nil {
			return err
		}
		inputs[i] = analysis.SeriesMapInput{Demo: demo, SHA256: digests[i]}
	}

	series, err := analysis.BuildSeries(bestOf, inputs)
	if err != nil {
		return err
	}
	lipgloss.Println(printstyle.StyleSuccess.Render("\n\nProcessing is done! \n"))
	fmt.Printf("Time taken for the series: %s\n\n", time.Since(startTime))

	// Selection narrows only what is shown and saved; the series teams,
	// scores and aggregates above were already resolved from everyone.
	var selected map[uint64]bool
	if len(players) > 0 {
		selected, err = analysis.SelectSeriesPlayers(series, players)
		if err != nil {
			return err
		}
	}

	dataexport.PrintSeriesCLITables(series, selected)
	if details {
		dataexport.PrintSeriesDetailTables(series, selected)
	}

	if save {
		seriesToSave := series
		if selected != nil {
			seriesToSave = analysis.FilterSeriesPlayers(series, selected)
		}
		return saveAndReport(func() (string, error) {
			return dataexport.WriteSeriesToFile(seriesToSave, saveType)
		})
	}
	return nil
}

// saveAndReport wraps a save-file writer with the shared status output the
// single-demo and series paths both print.
func saveAndReport(write func() (string, error)) error {
	lipgloss.Println(printstyle.StyleSuccess.Render("\nWritting data to file..."))
	fileName, err := write()
	if err != nil {
		return fmt.Errorf("writing to file: %w", err)
	}
	lipgloss.Println(printstyle.StyleSuccess.Render("Data written to file: " + fileName))
	return nil
}
