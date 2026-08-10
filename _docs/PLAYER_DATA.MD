# Player data

The `--save` flag writes every analysed player to a file. JSON contains all fields below; CSV contains a flattened selection of them plus the computed K/D. Stats never include warmup. Bots are never listed, but kills on bots count like any other kill.

## Fields

### Top level

| Field | JSON key | Description |
| --- | --- | --- |
| SteamID | `steam_id` | 64-bit SteamID of the player. |
| Name | `name` | Last name the player used in the demo. |
| UserID | `user_id` | Server user ID in this demo. |
| Deaths | `deaths` | Deaths, including suicides and world deaths (fall damage, C4). |

### Kill stats (`kill_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| Total | `total` | Enemy kills. Teamkills are not included. |
| HeadShots | `headshots` | Enemy kills that were headshots. |
| Precision | `precision` | Headshot ratio: `headshots / total`, from 0 to 1. Shown as a percentage in the table and CSV. |
| WeaponsKills | `weapons_kills` | Kills per weapon name. |
| TradeKills | `trade_kills` | Kills on the enemy who had killed a teammate earlier in the same round, within 5 seconds of that death. See [trade window](#trade-window) for the exact rule. |
| TeamKills | `team_kills` | Kills on teammates. |

#### Trade window

A kill is a trade when it lands on the *specific* enemy who killed a teammate, within 5 seconds of that teammate's death. In detail:

- It must be that killer. Killing any other enemy inside the window is not a trade.
- Both deaths have to fall in the same round.
- One kill counts once, even when the enemy it lands on had killed two teammates.
- Kills after the round is decided still count, so exit frags can be trades.

The 5-second window is a deliberate choice, not a value read out of the demo: HLTV states 5 seconds for Rating 3.0 ("two kills within 5 seconds") and Refrag documents the same for its Trades column. Other trackers differ — Leetify builds 4-second kill chains instead — so trade counts are not comparable between tools even when the label matches. If you are forking and your league counts trades differently, the window is the `tradeWindow` constant at the top of `cmd/demo_parser/round_tracker.go`.

### Assist stats (`assist_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| Total | `total` | Assists on enemy kills, flash assists included. |
| FlashedEnemies | `flashed_enemies` | Flash assists: the victim was blinded by this player's flashbang when killed. |
| DamageGiven | `damage_given` | Health damage dealt to enemies. Team damage, self damage and bomb damage never count (HLTV convention). |
| ADR | `adr` | Average damage per round: `damage_given / total rounds of the match`. |

### Map stats (`player_map_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| MVPs | `mvps` | Round MVP awards. |
| ACEs | `aces` | Rounds with 5 or more enemy kills. |
| ClutchesWon | `clutches_won` | Rounds won after being the last player alive on the team against at least one enemy (1v1 counts). Winning by defuse or time counts — no kills required. |
| KAST | `kast` | Percentage of the match's rounds with a Kill, Assist, Survival or Traded death (a teammate killed your killer within 5 seconds). Dying before the round officially ends cancels survival — including exit frags and the bomb going off after the whistle (HLTV convention). Match-end cleanup kills do not count. |

### Opening duel stats (`opening_duel_stats`)

The opening duel is the first enemy kill of a round. Teamkills, suicides and world deaths never open a round, and neither does a kill landing after the round is already decided. The table shows opening kills and deaths as the `Entry` column, e.g. `5:3`.

| Field | JSON key | Description |
| --- | --- | --- |
| OpeningKills | `opening_kills` | Rounds opened by a kill from this player: `{total, ct, t}`, split by the side the player was on. |
| OpeningDeaths | `opening_deaths` | Rounds opened by this player's death, with the same side split. |
| OpeningSuccessRate | `opening_success_rate` | Percentage of the player's opening kills whose round their team went on to win. 0 without any opening kills. This is a rate, not a count — some trackers use "opening success" to mean the opening-kill tally instead. |

## Match data

The JSON table output also carries match-level data:

| Field | JSON key | Description |
| --- | --- | --- |
| MapName | `map_name` | Map the demo was played on. |
| TotalRounds | `total_rounds` | Rounds played. |
| RoundsWonCT | `rounds_won_ct` | Final score of the side finishing as CT. |
| RoundsWonT | `rounds_won_t` | Final score of the side finishing as T. |
