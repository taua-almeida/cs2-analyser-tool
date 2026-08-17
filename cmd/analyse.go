package cmd

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
	dataexport "github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"
	filepicker "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/file-picker"
	multiselect "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/multi-select"
	printstyle "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/print-style"
	"github.com/taua-almeida/cs2-analyser-tool/internal/history"
)

var players []string   // players is the list of players to analyse.
var demoPaths []string // demoPaths are the demo files, in played order for a series.
var bestOf int         // bestOf is the series format: 0 for a single demo, else 3 or 5.
var save bool          // save is the flag to save the demo players data.
var saveType string    // saveType is the type of storage to use.
var details bool       // details prints the stats that do not fit the main table.

func init() {
	// Add the analyse command as a subcommand of rootCmd.
	rootCmd.AddCommand(analyseCmd)

	// StringArray rather than StringSlice: repeated --demo flags keep their
	// command-line order and a path containing a comma stays one path.
	analyseCmd.Flags().StringArrayVarP(&demoPaths, "demo", "d", nil, "Demo path. Repeat in played order for a --best-of series.")
	analyseCmd.Flags().IntVar(&bestOf, "best-of", 0, "Series format for multiple demos: 3 or 5.")
	analyseCmd.Flags().StringSliceVarP(&players, "players", "p", []string{}, "Players to analyse.")
	analyseCmd.Flags().BoolVarP(&save, "save", "s", false, "Save the demo players data.")
	analyseCmd.Flags().StringVar(&saveType, "save-type", "json", "Type of file to save the data [json, csv], default is json.")
	analyseCmd.Flags().BoolVar(&details, "details", false, "Print the extra stat tables that do not fit the main one.")
}

var analyseCmd = &cobra.Command{
	Use:          "analyse",
	Short:        "Analyse a CS2 game demo.",
	Long:         "Parse a CS2 demo and display its statistics. Run '" + rootCmd.Name() + " history' to list previous analyses.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if saveType != "json" && saveType != "csv" {
			return fmt.Errorf("invalid --save-type %q, must be json or csv", saveType)
		}
		bestOfSet := cmd.Flags().Changed("best-of")
		if err := validateSeriesFlags(bestOf, bestOfSet, len(demoPaths)); err != nil {
			return err
		}
		if bestOfSet {
			return runSeriesAnalysis(cmd.Context(), bestOf, demoPaths)
		}

		demoPath := ""
		if len(demoPaths) == 1 {
			demoPath = demoPaths[0]
		}
		if demoPath == "" {
			selected, err := filepicker.PickDemoFile()
			if err != nil {
				return fmt.Errorf("picking demo file: %w", err)
			}
			if selected == "" {
				fmt.Println("No file was selected :(. \n Ending program...")
				return nil
			}
			demoPath = selected
		}

		lipgloss.Println(printstyle.StyleInfo.Render("Processing CS2 demo, hang tight... \n"))

		startTime := time.Now()
		processedDemoData, digest, err := analyseAndHashDemoFile(cmd.Context(), demoPath)
		if err != nil {
			return err
		}
		// The stored analysis time is when parsing finished — captured now,
		// before the interactive player selection below can sit open for
		// minutes or hours and skew the history's ordering.
		analysedAt := time.Now().UTC()

		lipgloss.Println(printstyle.StyleSuccess.Render("\n\nProcessing is done! \n"))
		fmt.Printf("Time taken for the analysis: %s\n\n", time.Since(startTime))

		availablePlayers := analysis.GetPlayersName(processedDemoData.Players)
		if len(availablePlayers) == 0 {
			return fmt.Errorf("no players found in demo %s", demoPath)
		}

		if len(players) == 0 {
			selection := &multiselect.Selection{}
			program := tea.NewProgram(multiselect.InitialModelMultiSelect(
				"No players were selected, select the players you want to analyse:",
				availablePlayers, selection),
			)
			if _, err := program.Run(); err != nil {
				return fmt.Errorf("running player selection: %w", err)
			}
			players = selection.SelectedChoices
		}

		// An explicit choice — the flag or a nonempty multiselect — becomes
		// this match's stored display preference; analysing everyone stores
		// none, which reads back as everyone.
		explicitSelection := len(players) > 0
		if len(players) == 0 {
			lipgloss.Println(printstyle.StyleInfo.Render("No players selected, analysing everyone."))
			players = availablePlayers
		}

		playersToAnalyse, err := analysis.GetPlayersToAnalyse(processedDemoData.Players, players)
		if err != nil {
			return err
		}

		dataexport.PrintCLIDataTable(playersToAnalyse, &processedDemoData.Map, processedDemoData.GameMode)
		if details {
			dataexport.PrintCLIDetailTables(playersToAnalyse)
		}

		var exportErr error
		if save {
			analysisToSave := *processedDemoData
			analysisToSave.Players = playersToAnalyse
			exportErr = saveAndReport(func() (string, error) {
				return dataexport.WriteAnalysisToFile(&analysisToSave, saveType)
			})
		}

		// The complete unfiltered map is stored whatever was selected or
		// exported; the rendered and exported output above stays valid even
		// when storage fails, so the errors are joined rather than one
		// hiding the other.
		var selectedIDs []uint64
		if explicitSelection {
			selectedIDs = slices.Sorted(maps.Keys(playersToAnalyse))
		}
		storeErr := storeAnalysedMaps(cmd.Context(), cmd.OutOrStdout(), []history.StoreMatchInput{{
			SHA256:           digest,
			AnalysedAt:       analysedAt,
			AnalysisVersion:  currentVersion(),
			Analysis:         processedDemoData,
			Facts:            processedDemoData.PlayerAggregationFacts(),
			SelectedSteamIDs: selectedIDs,
		}})
		return errors.Join(exportErr, storeErr)
	},
}
