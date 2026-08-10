# Player data

The `--save` flag writes every analysed player to a file. JSON contains all fields below; CSV contains a flattened selection of them plus the computed K/D. The CLI table shows a narrower selection still, with `--details` printing the rest in extra tables below it. Stats never include warmup. Bots are never listed, but kills on bots count like any other kill.

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
| MultiKills | `multi_kills` | Multi-kill rounds bucketed by kill count: `{k2, k3, k4, k5}`. See [multi-kill rounds](#multi-kill-rounds). |
| ClutchesWon | `clutches_won` | Rounds won after being the last player alive on the team against at least one enemy (1v1 counts). Winning by defuse or time counts — no kills required. |
| KAST | `kast` | Percentage of the match's rounds with a Kill, Assist, Survival or Traded death (a teammate killed your killer within 5 seconds). Dying before the round officially ends cancels survival — including exit frags and the bomb going off after the whistle (HLTV convention). Match-end cleanup kills do not count. |

#### Multi-kill rounds

`multi_kills` counts rounds, not kills. A round with three enemy kills adds one to `k3`, and nothing else.

- **Buckets are exclusive.** A 4k is only a 4k, never also a 2k and a 3k. So `k2 + k3 + k4 + k5` is the number of rounds the player got at least two enemy kills in, and each bucket is directly usable as one of the six sub-ratings of HLTV Rating 3.0.
- **Enemy kills only**, the same rule ACEs use. Teamkills never count, so four enemy kills plus a teamkill is a 4k.
- **A round with 0 or 1 kills is in no bucket.** The smallest multi-kill is the 2k.
- **Post-round kills count**, exit frags included, again matching ACEs.
- **`k5` and `aces` are the same rounds by construction.** Both are "5 or more enemy kills" — `k5` takes everything from 5 up rather than exactly 5, so the two can never disagree even in the freak case of a sixth kill after a disconnect and reconnect. The duplication is deliberate: `multi_kills` stays a complete, self-contained bucket set for the rating work, and `aces` stays where it has always been for anything already reading it.

The four counts are in the JSON, in the CSV as the `2K`/`3K`/`4K`/`5K` columns, and in the `Multi-kill rounds` table that `--details` prints. The main table does not show them; it is already wide enough.

### Opening duel stats (`opening_duel_stats`)

The opening duel is the first enemy kill of a round. Teamkills, suicides and world deaths never open a round, and neither does a kill landing after the round is already decided. The table shows opening kills and deaths as the `Entry` column, e.g. `5:3`.

| Field | JSON key | Description |
| --- | --- | --- |
| OpeningKills | `opening_kills` | Rounds opened by a kill from this player: `{total, ct, t}`, split by the side the player was on. |
| OpeningDeaths | `opening_deaths` | Rounds opened by this player's death, with the same side split. |
| OpeningSuccessRate | `opening_success_rate` | Percentage of the player's opening kills whose round their team went on to win. 0 without any opening kills. This is a rate, not a count — some trackers use "opening success" to mean the opening-kill tally instead. |

### Side split stats (`side_stats`)

The same core stats split by the side the player was on. Players swap sides at halftime, so the split is attributed round by round: a kill counts on the side the player held when they got it, never on the team they finished the match on.

| Field | JSON key | Description |
| --- | --- | --- |
| Rounds | `rounds` | Rounds the player was in, `{total, ct, t}`. Counted from the players on a side when live play begins, so picking a side during freeze time counts as playing the round. See [who plays a round](#who-plays-a-round). |
| Kills | `kills` | Enemy kills with the same side split. Teamkills are excluded, exactly as in `kill_stats.total`. |
| Deaths | `deaths` | Deaths with the same side split, suicides and world deaths included, exactly as in the top-level `deaths`. |
| ADR | `adr` | Damage per round on each side: `{ct, t}`. See [side denominators](#side-denominators). |
| KAST | `kast` | KAST percentage on each side: `{ct, t}`, same rule as the match-wide `kast`. |

`kills` and `deaths` always add back up to their match-wide counterparts, so `kills.ct + kills.t == kill_stats.total`.

#### Who plays a round

The roster is read twice: once at the round start, and again when freeze time ends. Freeze time runs after the round has already started, and a player who picks a side inside that window spawns and plays the round like everyone else, so the second read folds them in. Someone who swapped sides in that window plays the round on the side they end freeze time on, which is the side their kills count towards.

The second read never revives anyone. A player who disconnected during freeze time stays counted as having played the round, without survival credit, exactly as a mid-round disconnect is treated.

This also feeds the match-wide `kast`, which is why a freeze-time joiner's KAST rounds are no longer dropped either.

#### Side denominators

`adr` and `kast` are divided by `rounds.ct` and `rounds.t` — the rounds that player actually spent on that side. There is no alternative: sides swap at halftime, so no single match-wide CT round count applies to every player, and a substitute or a late connect plays a different number of rounds than the match has.

The match-wide `assist_stats.adr` and `player_map_stats.kast` keep dividing by the rounds of the whole match, following HLTV convention. **The two only reconcile for a player who was there for every round.** For anyone else, the per-side numbers are the higher, more flattering ones, because they are not diluted by the rounds the player missed. One of the test fixtures has a player who joined for the last 3 of 8 rounds: their match ADR is 59.6 and their T-side ADR is 159.0, and both are correct under their own rule.

`adr` and `kast` are 0 for a side the player never played, not null — a `rounds.ct` of 0 gives an `adr.ct` of 0.

Unlike the counts, these two are rates and so have no `total` field. The match-wide value is the existing `assist_stats.adr` / `player_map_stats.kast`.

## Match data

The JSON table output also carries match-level data:

| Field | JSON key | Description |
| --- | --- | --- |
| MapName | `map_name` | Map the demo was played on. |
| TotalRounds | `total_rounds` | Rounds played. |
| RoundsWonCT | `rounds_won_ct` | Final score of the side finishing as CT. |
| RoundsWonT | `rounds_won_t` | Final score of the side finishing as T. |
