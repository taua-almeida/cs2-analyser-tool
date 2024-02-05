package cmd

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	dataexport "github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"
	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
	filepicker "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/file-picker"
	multiselect "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/multi-select"
	printstyle "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/print-style"
)

var players []string // players is the list of players to analyse.
var demoPath string  // demoPath is the path to the demo file.
var save bool        // save is the flag to save the demo players data.
var saveType string  // saveType is the type of storage to use.

type Options struct {
	Players *multiselect.Selection
}

func init() {
	// Add the analyse command as a subcommand of rootCmd.
	rootCmd.AddCommand(analyseCmd)

	analyseCmd.Flags().StringVarP(&demoPath, "demo", "d", "", "Demo path.")
	analyseCmd.Flags().StringSliceVarP(&players, "players", "p", []string{}, "Players to analyse.")
	analyseCmd.Flags().BoolVarP(&save, "save", "s", false, "Save the demo players data.")
	analyseCmd.Flags().StringVarP(&saveType, "save-type", "", "json", "Type of file to save the data [json, csv], default is json.")
}

var analyseCmd = &cobra.Command{
	Use:   "analyse",
	Short: "Analyse a CS2 game demo.",
	Long:  "This command will parse your cs2 demo and give you some stats about it. Use history to see your previous demos.",
	Run: func(cmd *cobra.Command, args []string) {
		flagDemoPath := cmd.Flag("demo").Value.String()
		flagPlayers, err := cmd.Flags().GetStringSlice("players")
		if err != nil {
			fmt.Println("Error getting players flag: ", err)
			return
		}

		opts := &Options{
			Players: &multiselect.Selection{},
		}

		if flagDemoPath == "" {
			_, selectedFilePath := filepicker.InitialModelFilePicker()
			if selectedFilePath == "" {
				fmt.Println("No file was selected :(. \n Ending program...")
				return
			}
			flagDemoPath = selectedFilePath
		}

		fmt.Println(printstyle.StyleInfo.Render("Processing CS2 demo, hang tight... \n"))

		startTime := time.Now()
		processedDemoData := demoparser.ProcessDemo(flagDemoPath)
		endTime := time.Since(startTime)

		fmt.Println(printstyle.StyleSuceess.Render("\n\nProcessing is done! \n"))
		fmt.Printf("Time taken for ProcessDemo: %s\n\n", endTime)
		if len(flagPlayers) == 0 {
			program := tea.NewProgram(multiselect.InitialModelMultiSelect(
				"No players were selected, select the players you want to analyse:",
				demoparser.GetPlayersName(processedDemoData.Players), opts.Players),
			)
			if _, err := program.Run(); err != nil {
				fmt.Println("Error running program:", err)
				return
			}
			flagPlayers = opts.Players.SelectedChoices
		}

		playerToAnalyse := demoparser.GetPlayersToAnalyse(processedDemoData.Players, flagPlayers)

		dataexport.PrintCLIDataTable(playerToAnalyse, &processedDemoData.Map, processedDemoData.GameMode)

		if save {
			fmt.Println(printstyle.StyleSuceess.Render("\nWritting data to file..."))
			fileName, err := dataexport.WritePlayersToFile(playerToAnalyse, saveType)
			if err != nil {
				fmt.Println(printstyle.StyleError.Render(fmt.Sprintf("Error writing to file: %s", err)))
			}
			fmt.Println(printstyle.StyleSuceess.Render("Data written to file: ", fileName))
		}
	},
}
