package cmd

import (
	"fmt"

	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	demoparser "github.com/taua-almeida/cs2-analyser-tool/cmd/demo_parser"
	filepicker "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/file-picker"
	multiselect "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/multi-select"
	printstyle "github.com/taua-almeida/cs2-analyser-tool/cmd/ui/print-style"

	"github.com/jedib0t/go-pretty/v6/table"
)

var players []string // players is the list of players to analyse.
var demoPath string  // demoPath is the path to the demo file.
var store bool       // store is the flag to store the demo players data.

type Options struct {
	Players *multiselect.Selection
}

func init() {
	// Add the analyse command as a subcommand of rootCmd.
	rootCmd.AddCommand(analyseCmd)

	analyseCmd.Flags().StringVarP(&demoPath, "demo", "d", "", "Demo path.")
	analyseCmd.Flags().StringSliceVarP(&players, "players", "p", []string{}, "Players to analyse.")
	analyseCmd.Flags().BoolVarP(&store, "store", "s", false, "Store the demo players data.")
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

		allDemoPlayers := demoparser.ProcessDemo(flagDemoPath)

		fmt.Println(printstyle.StyleSuceess.Render("\n\nProcessing is done!"))

		if len(flagPlayers) == 0 {
			program := tea.NewProgram(multiselect.InitialModelMultiSelect("No players were selected, select the players you want to analyse:", demoparser.GetPlayersName(allDemoPlayers), opts.Players))
			if _, err := program.Run(); err != nil {
				fmt.Println("Error running program:", err)
				return
			}
			flagPlayers = opts.Players.SelectedChoices
		}

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{"Name", "Kills", "Deaths", "K/D", "HS", "Assists", "Flash Assist", "Damage Given", "Precision (%)", "Best Weapon"})

		playerToAnalyse := demoparser.GetPlayersToAnalyse(allDemoPlayers, flagPlayers)

		for _, player := range playerToAnalyse {
			playerBestWeapon := demoparser.GetPlayerBestWeapon(player.KillStats.WeaponsKills)
			kd := fmt.Sprintf("%.3f", float32(player.KillStats.Total)/float32(player.Deaths))
			t.AppendRow(table.Row{
				player.Name,
				player.KillStats.Total,
				player.Deaths,
				kd,
				player.KillStats.HeadShots,
				player.AssistStats.Total,
				player.AssistStats.FlashedEnemies,
				player.AssistStats.DamageGiven,
				int(player.KillStats.Precision * 100),
				playerBestWeapon,
			})
		}
		t.SortBy([]table.SortBy{{Name: "Kills", Mode: table.DscNumeric}})
		t.Render()

		if store {
			fmt.Println(printstyle.StyleSuceess.Render("\nWritting data to file..."))
			fileName, err := demoparser.WritePlayersToFile(playerToAnalyse)
			if err != nil {
				fmt.Println(printstyle.StyleError.Render(fmt.Sprintf("Error writing to file: %s", err)))
			}
			fmt.Println(printstyle.StyleSuceess.Render("Data written to file: ", fileName))
		}
	},
}
