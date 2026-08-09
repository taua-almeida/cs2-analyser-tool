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
| TradeKills | `trade_kills` | Kills on an enemy who had killed a teammate within the previous 5 seconds. |
| TeamKills | `team_kills` | Kills on teammates. |

### Assist stats (`assist_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| Total | `total` | Assists on enemy kills, flash assists included. |
| FlashedEnemies | `flashed_enemies` | Flash assists: the victim was blinded by this player's flashbang when killed. |
| DamageGiven | `damage_given` | Health damage dealt to enemies. Team damage and self damage never count. |
| ADR | `adr` | Average damage per round: `damage_given / total rounds of the match`. |

### Map stats (`player_map_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| MVPs | `mvps` | Round MVP awards. |
| ACEs | `aces` | Rounds with 5 or more enemy kills. |
| ClutchesWon | `clutches_won` | Rounds won after being the last player alive on the team against at least one enemy (1v1 counts). Winning by defuse or time counts — no kills required. |
| KAST | `kast` | Percentage of the match's rounds with a Kill, Assist, Survival or Traded death (a teammate killed your killer within 5 seconds). An enemy exit frag in the post-round seconds cancels survival; dying to the bomb after the round is decided does not. |

## Match data

The JSON table output also carries match-level data:

| Field | JSON key | Description |
| --- | --- | --- |
| MapName | `map_name` | Map the demo was played on. |
| TotalRounds | `total_rounds` | Rounds played. |
| RoundsWonCT | `rounds_won_ct` | Final score of the side finishing as CT. |
| RoundsWonT | `rounds_won_t` | Final score of the side finishing as T. |
