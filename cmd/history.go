// This file is the history side of issue #7: the automatic post-analysis
// storage hook and the commands that read it back. All SQL lives in
// internal/history; this file only assembles inputs and presents results.
package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
	dataexport "github.com/taua-almeida/cs2-analyser-tool/cmd/dataexport"
	"github.com/taua-almeida/cs2-analyser-tool/internal/history"
)

var historyShowDetails bool // historyShowDetails prints the extra stat tables of a stored match.

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.AddCommand(historyShowCmd)
	historyShowCmd.Flags().BoolVar(&historyShowDetails, "details", false, "Print the extra stat tables that do not fit the main one.")
}

var historyCmd = &cobra.Command{
	Use:          "history",
	Short:        "List analysed matches stored in the local history.",
	Long:         "Every successful analysis is stored in a local SQLite database, deduplicated by the demo's SHA-256. This lists them newest analysis first. Times are when each demo was analysed, not when the match was played.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openHistory(cmd.Context())
		if err != nil {
			return err
		}
		defer db.Close()
		return runHistoryList(cmd.Context(), db, cmd.OutOrStdout())
	},
}

var historyShowCmd = &cobra.Command{
	Use:          "show <id>",
	Short:        "Re-render a stored match without its demo.",
	Long:         "Renders a stored analysis from the local history. <id> is the match's SHA-256 or a unique prefix of at least 8 characters; the stored display preference narrows the view, and without one everyone is shown.",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openHistory(cmd.Context())
		if err != nil {
			return err
		}
		defer db.Close()
		return runHistoryShow(cmd.Context(), db, cmd.OutOrStdout(), args[0], historyShowDetails)
	},
}

// openHistory opens the history database at its resolved location.
func openHistory(ctx context.Context) (*history.DB, error) {
	dir, err := history.DefaultDir()
	if err != nil {
		return nil, err
	}
	return history.Open(ctx, dir)
}

// runHistoryList prints the stored matches newest analysis first. An empty
// history is a message, not an error.
func runHistoryList(ctx context.Context, db *history.DB, output io.Writer) error {
	matches, err := db.ListMatches(ctx)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		fmt.Fprintf(output, "No matches in history yet. Run '%s analyse' and successful analyses are stored automatically.\n", rootCmd.Name())
		return nil
	}
	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.AppendHeader(table.Row{"ID", "Analysed", "Map", "Score", "Game mode"})
	for _, match := range matches {
		t.AppendRow(table.Row{
			match.ShortID(),
			formatAnalysedAt(match.AnalysedAt),
			match.MapName,
			match.Score(),
			match.GameMode,
		})
	}
	plural := "es"
	if len(matches) == 1 {
		plural = ""
	}
	t.SetCaption("%d stored match%s. Times are analysis times in your local time zone.\n", len(matches), plural)
	t.Render()
	return nil
}

// runHistoryShow re-renders one stored match from the database alone — the
// original demo is never opened. The stored display preference narrows only
// the view; the canonical stored data is not touched.
func runHistoryShow(ctx context.Context, db *history.DB, output io.Writer, id string, details bool) error {
	match, err := db.MatchByID(ctx, id)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Match %s, analysed %s with %s\n",
		match.ShortID(), formatAnalysedAt(match.AnalysedAt), match.AnalysisVersion)

	players := match.Analysis.Players
	if match.SelectionExplicit {
		// A fresh map narrows the view — possibly to nobody, when a series
		// selection kept no player of this map; the decoded canonical
		// analysis stays complete.
		selected := make(map[uint64]*analysis.DemoPlayer, len(match.SelectedSteamIDs))
		for _, steamID := range match.SelectedSteamIDs {
			if player := players[steamID]; player != nil {
				selected[steamID] = player
			}
		}
		fmt.Fprintf(output, "Showing %d of %d stored players (stored display preference).\n",
			len(selected), len(players))
		players = selected
	}
	dataexport.FprintCLIDataTable(output, players, &match.Analysis.Map, match.Analysis.GameMode)
	if details {
		dataexport.FprintCLIDetailTables(output, players)
	}
	return nil
}

// formatAnalysedAt renders a stored UTC analysis time in the user's local
// time zone. It is always the analysis time — no demo records a trustworthy
// match time.
func formatAnalysedAt(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05")
}

// storeAnalysedMaps stores every supplied map in the local history, opening
// the database once, and reports each match's outcome. Callers run it after
// the analysis is rendered and any export written, so a storage failure
// never takes the visible results with it — it comes back as this call's
// error for the caller to join with whatever else failed.
func storeAnalysedMaps(ctx context.Context, output io.Writer, inputs []history.StoreMatchInput) error {
	db, err := openHistory(ctx)
	if err != nil {
		return fmt.Errorf("storing analysis in history: %w", err)
	}
	defer db.Close()

	for _, input := range inputs {
		result, err := db.StoreMatch(ctx, input)
		if err != nil {
			return fmt.Errorf("storing analysis %s in history: %w", input.SHA256, err)
		}
		if result.Created {
			fmt.Fprintf(output, "History: stored match %s\n", input.SHA256[:12])
		} else {
			fmt.Fprintf(output, "History: match %s was already stored; updated its display preference\n", input.SHA256[:12])
		}
	}
	return nil
}
