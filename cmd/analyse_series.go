package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
	dataexport "github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"
	printstyle "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/print-style"
	"github.com/taua-almeida/cs2-analyser-tool/internal/history"
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

// parseSeriesMaps parses every series demo in supplied order through the
// shared hash-while-parse helper, so each file is opened once and its digest
// covers exactly the bytes that were analysed. Repeated demo content is
// rejected as soon as the second copy's digest is known, before any further
// map is parsed.
func parseSeriesMaps(ctx context.Context, paths []string) ([]analysis.SeriesMapInput, error) {
	inputs := make([]analysis.SeriesMapInput, len(paths))
	seen := make(map[string]int, len(paths))
	for i, path := range paths {
		fmt.Printf("Parsing map %d/%d: %s\n", i+1, len(paths), path)
		demo, digest, err := analyseAndHashDemoFile(ctx, path)
		if err != nil {
			return nil, err
		}
		if err := recordSeriesDigest(seen, paths, i, digest); err != nil {
			return nil, err
		}
		inputs[i] = analysis.SeriesMapInput{Demo: demo, SHA256: digest}
	}
	return inputs, nil
}

// recordSeriesDigest registers one parsed map's content digest, rejecting a
// digest already supplied earlier in the series so the same map under two
// paths fails before any further parsing.
func recordSeriesDigest(seen map[string]int, paths []string, index int, digest string) error {
	if first, duplicate := seen[digest]; duplicate {
		return fmt.Errorf("--demo %s repeats the content of %s (SHA-256 %s)", paths[index], paths[first], digest)
	}
	seen[digest] = index
	return nil
}

// runSeriesAnalysis is the multi-demo path: parse and hash in command-line
// order, build the series, resolve the player selection against cross-map
// identity, then render and optionally save. It never opens the demo file
// picker or the interactive player selector; without --players every player
// is analysed.
func runSeriesAnalysis(ctx context.Context, bestOf int, paths []string) error {
	lipgloss.Println(printstyle.StyleInfo.Render("Processing CS2 series, hang tight... \n"))
	startTime := time.Now()
	inputs, err := parseSeriesMaps(ctx, paths)
	if err != nil {
		return err
	}
	// The stored analysis time is when parsing finished, captured before
	// rendering and export can add their own delay.
	analysedAt := time.Now().UTC()

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

	var exportErr error
	if save {
		seriesToSave := series
		if selected != nil {
			seriesToSave = analysis.FilterSeriesPlayers(series, selected)
		}
		exportErr = saveAndReport(func() (string, error) {
			return dataexport.WriteSeriesToFile(seriesToSave, saveType)
		})
	}

	// Every complete map is stored individually — the validated series
	// aggregate itself is never stored, so trends cannot double count. The
	// storage error joins the export one so neither hides the other, and
	// the rendered series above stays valid either way.
	storeErr := storeAnalysedMaps(ctx, os.Stdout, seriesStoreInputs(inputs, selected, analysedAt))
	return errors.Join(exportErr, storeErr)
}

// seriesStoreInputs assembles the history record of each played map once
// BuildSeries has accepted the series: the complete unfiltered analysis with
// its digest and exact facts, and — per map — the selected players that
// actually appear in it. With an explicit series selection the per-map IDs
// stay non-nil even when nobody selected played that map, so the stored
// view is the same empty one the live series rendering shows, never a
// fallback to everyone.
func seriesStoreInputs(inputs []analysis.SeriesMapInput, selected map[uint64]bool,
	analysedAt time.Time) []history.StoreMatchInput {
	stores := make([]history.StoreMatchInput, len(inputs))
	for i, input := range inputs {
		var selectedIDs []uint64
		if selected != nil {
			selectedIDs = make([]uint64, 0, len(selected))
			for id := range selected {
				if input.Demo.Players[id] != nil {
					selectedIDs = append(selectedIDs, id)
				}
			}
		}
		slices.Sort(selectedIDs)
		stores[i] = history.StoreMatchInput{
			SHA256:           input.SHA256,
			AnalysedAt:       analysedAt,
			AnalysisVersion:  currentVersion(),
			Analysis:         input.Demo,
			Facts:            input.Demo.PlayerAggregationFacts(),
			SelectedSteamIDs: selectedIDs,
		}
	}
	return stores
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
