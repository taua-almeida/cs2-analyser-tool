// This file is the trend side of issue #7: the compare command renders a
// player's Premier trend from the local history. Identity resolution and the
// exact additive arithmetic live in internal/history; this file only
// presents them.
package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"github.com/taua-almeida/cs2-analyser-tool/analysis"
	"github.com/taua-almeida/cs2-analyser-tool/internal/history"
)

var comparePlayer string // comparePlayer is the SteamID64 or alias to build the trend for.

func init() {
	rootCmd.AddCommand(compareCmd)
	compareCmd.Flags().StringVar(&comparePlayer, "player", "", "SteamID64 or stored alias of the player to compare.")
	// The flag is registered above, so marking it required cannot fail.
	_ = compareCmd.MarkFlagRequired("player")
}

var compareCmd = &cobra.Command{
	Use:          "compare --player <steam-id-or-name>",
	Short:        "Show a player's Premier trend from the local history.",
	Long:         "Aggregates every stored map explicitly recorded as premier that the player appears in, chronologically by analysis time. Aggregate rates are recomputed from exact additive facts, never averaged from per-map values. --player takes a nonzero decimal SteamID64 first; anything else must match exactly one stored alias (case-insensitively).",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openHistory(cmd.Context())
		if err != nil {
			return err
		}
		defer db.Close()
		return runCompare(cmd.Context(), db, cmd.OutOrStdout(), comparePlayer)
	},
}

// runCompare resolves the player and renders their Premier trend: one row
// per stored premier map in analysis order, and a totals row recomputed from
// the exact additive facts.
func runCompare(ctx context.Context, db *history.DB, output io.Writer, player string) error {
	steamID, err := db.ResolvePlayer(ctx, player)
	if err != nil {
		return err
	}
	trend, err := db.PremierTrend(ctx, steamID)
	if err != nil {
		return err
	}
	if len(trend.Matches) == 0 {
		fmt.Fprintf(output, "No stored premier matches include SteamID %d. Only maps recorded as premier count towards trends.\n", steamID)
		return nil
	}

	plural := "s"
	if trend.Totals.Maps == 1 {
		plural = ""
	}
	fmt.Fprintf(output, "Premier trend for SteamID %d over %d map%s (%d rounds), by analysis time\n",
		trend.SteamID, trend.Totals.Maps, plural, trend.Totals.Rounds)
	fmt.Fprintf(output, "Known as: %s\n", strings.Join(trend.Aliases, ", "))

	t := table.NewWriter()
	t.SetOutputMirror(output)
	t.AppendHeader(table.Row{"Analysed", "Map", "Score", "Alias", "K", "D", "K/D", "ADR",
		"KAST (%)", "HS (%)", "Entry", "Open (%)", "Trade kills", "Deaths traded",
		"Util dmg", "Enemies flashed", "Nades"})
	for _, match := range trend.Matches {
		player := match.Player
		t.AppendRow(table.Row{
			formatAnalysedAt(match.AnalysedAt),
			match.MapName,
			match.Score(),
			match.Alias,
			player.KillStats.Total,
			player.Deaths,
			formatKD(player.KillStats.Total, player.Deaths),
			fmt.Sprintf("%.1f", player.AssistStats.ADR),
			fmt.Sprintf("%.1f", player.PlayerMapStats.KAST),
			fmt.Sprintf("%.1f", player.KillStats.Precision*100),
			formatEntry(player.OpeningDuelStats),
			fmt.Sprintf("%.1f", player.OpeningDuelStats.OpeningSuccessRate),
			player.KillStats.TradeKills,
			player.DeathsTraded.Total,
			player.UtilityStats.UtilityDamage.Total,
			player.UtilityStats.EnemiesFlashed,
			player.UtilityStats.GrenadesThrown.Total,
		})
	}
	totals := trend.Totals
	t.AppendFooter(table.Row{
		"Total",
		fmt.Sprintf("%d maps", totals.Maps),
		fmt.Sprintf("%d rounds", totals.Rounds),
		"",
		totals.Kills,
		totals.Deaths,
		fmt.Sprintf("%.3f", totals.KD()),
		fmt.Sprintf("%.1f", totals.ADR()),
		fmt.Sprintf("%.1f", totals.KASTPercent()),
		fmt.Sprintf("%.1f", totals.HSPercent()),
		fmt.Sprintf("%d:%d", totals.OpeningKills, totals.OpeningDeaths),
		fmt.Sprintf("%.1f", totals.OpeningSuccessPercent()),
		totals.TradeKills,
		totals.DeathsTraded,
		totals.UtilityDamage,
		totals.EnemiesFlashed,
		totals.GrenadesThrown,
	})
	t.SetCaption("Totals are exact sums; total K/D, ADR, KAST, HS%% and opening success are recomputed from those sums, never averaged.\n")
	t.Render()
	return nil
}

// formatKD matches the analyse table's K/D convention: zero deaths divide by
// one so the ratio stays finite.
func formatKD(kills, deaths int) string {
	if deaths == 0 {
		deaths = 1
	}
	return fmt.Sprintf("%.3f", float64(kills)/float64(deaths))
}

// formatEntry formats opening duels as kills:deaths.
func formatEntry(opening analysis.OpeningDuelStats) string {
	return fmt.Sprintf("%d:%d", opening.OpeningKills.Total, opening.OpeningDeaths.Total)
}
