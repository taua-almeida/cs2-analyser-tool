package cmd

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
	dataexport "github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"
	filepicker "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/file-picker"
	multiselect "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/multi-select"
	printstyle "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/print-style"
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
	Long:         "This command will parse your cs2 demo and give you some stats about it. Use history to see your previous demos.",
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
		processedDemoData, err := analysis.AnalyseFile(cmd.Context(), demoPath)
		if err != nil {
			return err
		}

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

		if save {
			analysisToSave := *processedDemoData
			analysisToSave.Players = playersToAnalyse
			return saveAndReport(func() (string, error) {
				return dataexport.WriteAnalysisToFile(&analysisToSave, saveType)
			})
		}
		return nil
	},
}
