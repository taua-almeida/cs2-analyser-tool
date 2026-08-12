# Player data

The `--save` flag writes every analysed player to a file. JSON contains all fields below; CSV contains a flattened selection of them plus the computed K/D. The CLI table shows a narrower selection still, with `--details` printing the rest in extra tables below it. Stats never include warmup. Bots are never listed, but kills on bots count like any other kill.

## Fields

### Top level

| Field | JSON key | Description |
| --- | --- | --- |
| SteamID | `steam_id` | 64-bit SteamID of the player. |
| Name | `name` | Last name the player used in the demo. |
| UserID | `user_id` | Server user ID in this demo. |
| Deaths | `deaths` | Deaths, including suicides, ordinary World deaths such as falls, and C4 deaths. A round-ending C4 blast can emit deaths after `RoundEnd`; they still count during `GamePhaseStartGamePhase`. A C4 death emitted after `RoundEnd` during `GamePhaseGameHalfEnded`, plus match-end World cleanup, is excluded. |
| DeathsTraded | `deaths_traded` | Per-round traded-death count: `{total, ct, t}`. A round contributes once when at least one of the player's deaths was avenged inside the trade window. |

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
- One kill gives its player one `trade_kill`, even when the enemy it lands on had killed two teammates. Each qualifying teammate gets one `deaths_traded`, so that one kill can trade two deaths.
- Post-round events deliberately remain eligible: a death after the round is decided can be traded, and a revenge kill after the round can trade an earlier death, as long as both events stay in this round and inside the window. This differs from opening duels, which cannot start after the round is decided.

`deaths_traded` lives at the player top level beside `deaths` because it describes what happened to the player who died, not a kill they made. It is a per-round count, not a per-death count: the tracker records a boolean per player, so a round adds at most one even when a respawn mode lets that player die and be traded more than once. Its CT/T split always uses the side that player held in that round, never the team they finished the match on. No separate rate is stored. `deaths_traded.total / deaths` is only a per-death rate in modes without mid-round respawns; in respawn modes the numerator stays per-round while `deaths` counts every death.

The 5-second window is a deliberate choice, not a value read out of the demo: HLTV states 5 seconds for Rating 3.0 ("two kills within 5 seconds") and Refrag documents the same for its Trades column. Other trackers differ — Leetify builds 4-second kill chains instead — so trade counts are not comparable between tools even when the label matches. If you are forking and your league counts trades differently, the window is the `tradeWindow` constant at the top of `cmd/demo_parser/round_tracker.go`.

The count is in JSON, in CSV as `Deaths Traded` plus its CT/T columns, and in the `Trade stats` table that `--details` prints. The main table stays unchanged.

### Assist stats (`assist_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| Total | `total` | Assists on enemy kills, flash assists included. |
| FlashedEnemies | `flashed_enemies` | Flash assists: the victim was blinded by this player's flashbang when killed. |
| DamageGiven | `damage_given` | Health damage credited against enemies. Team damage, self damage and bomb damage never count (HLTV convention). An attacker-less World event is credited to the enemy who damaged the same victim in the same or immediately preceding demo frame; isolated World damage and match-end cleanup are excluded. |
| ADR | `adr` | Average damage per round: `damage_given / total rounds of the match`. |

### Utility stats (`utility_stats`)

Match-wide use and effectiveness of the six standard purchasable grenades: flashbang, smoke, HE, molotov, incendiary and decoy. Game-mode equipment such as snowballs, breach charges and bump mines is excluded. The first version has no CT/T split, per-round rate or self-flash field.

| Field | JSON key | Description |
| --- | --- | --- |
| EnemiesFlashed | `enemies_flashed` | Enemy `PlayerFlashed` events attributed to this player. This counts events, so flashing the same enemy twice adds two. |
| FriendsFlashed | `friends_flashed` | Teammate `PlayerFlashed` events attributed to this player. Self-flashes are excluded. |
| EnemyFlashTimeSeconds | `enemy_flash_time_seconds` | Sum of new blind time added by enemy flash events, in seconds. When a blinded player is re-flashed, already-covered time is not counted twice. |
| AverageEnemyFlashTimeSeconds | `average_enemy_flash_time_seconds` | `enemy_flash_time_seconds / enemies_flashed`, or 0 when no enemy was flashed. It is derived after parsing so the count and total duration remain the source values. |
| UtilityDamage | `utility_damage` | Actual enemy health removed by damaging utility: `{total, he, fire}`. See [utility damage](#utility-damage). |
| GrenadesThrown | `grenades_thrown` | Uses of standard grenades: `{total, flash, smoke, he, molotov, incendiary, decoy}`. See [grenades thrown](#grenades-thrown). |
| UnusedUtilityValue | `unused_utility_value` | Cumulative purchase value of standard grenades carried at qualifying deaths. See [unused utility value](#unused-utility-value). |

Flash assists remain at `assist_stats.flashed_enemies`; `utility_stats` does not duplicate them. The `Utility effectiveness` table shown by `--details` places that existing count beside the new flash, damage and unused-value fields.

#### Utility damage

Utility damage reuses the same per-event real-health cap as `assist_stats.damage_given`. It never exceeds either the damage reported for the event or the victim's actual health drop. Self damage, team damage and bomb damage are excluded from both totals.

- `he` counts HE grenade damage.
- `fire` combines molotov and incendiary damage. Inferno hurt events do not reliably identify which fire grenade created the flames, although their throw counts remain separate.
- `total` is `he + fire`.
- Gun damage and direct projectile chip damage from otherwise non-damaging grenades do not count.

Smoke and decoy therefore have use counts but no invented effectiveness value.

#### Grenades thrown

Each `GrenadeProjectileThrow` adds one use. `WeaponFire` is not counted, and warmup events are ignored. `total` is the sum of the six named counts. If demoinfocs cannot resolve a projectile model, the analyser recovers the type from the thrower's active inventory item when possible. Smoke and decoy are represented by throws only.

#### Unused utility value

This is a cumulative value across death events, not an average and not the largest inventory seen at one death. Every qualifying event contributes, including repeated deaths in respawn modes, suicides, bomb and fall deaths, and combat deaths after the round result. A C4 event emitted after `RoundEnd` during `GamePhaseGameHalfEnded` still contributes here even though it is excluded from raw player death totals. Automatic World cleanup after the round has already been decided is excluded.

Only standard grenade inventory is valued. Weapons, armour and game-mode equipment do not contribute. Quantity is `AmmoInMagazine + AmmoReserve`, which counts a second flashbang stored as reserve ammo. The pinned Mirage case in `TestProcessDemoGolden` explicitly verifies that the demoinfocs version used by this project still exposes the victim's pre-death inventory during `Kill`, without parsing the fixture a second time.

`Equipment` has no price API, so `cmd/demo_parser/utility.go` owns one explicit price table. It uses normal Competitive buy-menu prices from the August 2026 CS2 economy:

| Grenade | Price |
| --- | ---: |
| Flashbang | $200 |
| Smoke | $300 |
| HE | $300 |
| Molotov | $400 |
| Incendiary | $500 |
| Decoy | $50 |

Every entry is unit-tested. Update the table and this convention together if Valve changes the economy.

All utility scalars are appended to CSV after the existing columns. `--details` adds `Utility effectiveness` and `Grenades thrown` tables; the main table is unchanged.

### Map stats (`player_map_stats`)

| Field | JSON key | Description |
| --- | --- | --- |
| MVPs | `mvps` | Round MVP awards. |
| ACEs | `aces` | Rounds with 5 or more enemy kills. |
| MultiKills | `multi_kills` | Multi-kill rounds bucketed by kill count: `{k2, k3, k4, k5}`. See [multi-kill rounds](#multi-kill-rounds). |
| ClutchesWon | `clutches_won` | Rounds won after being the last player alive on the team against at least one enemy (1v1 counts). Winning by defuse or time counts — no kills required. |
| KAST | `kast` | Percentage of the match's rounds with a Kill, Assist, Survival or Traded death (a teammate killed your killer within 5 seconds). `RoundEnd` does not award survival immediately: exit frags and C4 deaths before `RoundEndOfficial` still cancel it. This round/KAST treatment is separate from raw death totals, where a C4 event emitted after `RoundEnd` during `GamePhaseGameHalfEnded` is excluded. Match-end World cleanup does not cancel survival. |

#### Multi-kill rounds

`multi_kills` counts rounds, not kills. A round with three enemy kills adds one to `k3`, and nothing else.

- **Buckets are exclusive.** A 4k is only a 4k, never also a 2k and a 3k. So `k2 + k3 + k4 + k5` is the number of rounds the player got at least two enemy kills in, and the buckets feed the [rating](#rating-rating)'s multi-kill sub-rating directly.
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

### Rating (`rating`)

An HLTV Rating 3.0-style rating. **HLTV's exact formula is proprietary, so this is an approximation**: it follows the structure HLTV has described publicly — six sub-ratings, most of them economy-adjusted — with every constant chosen here and calibrated so an average player lands near 1.00. Do not expect it to match the number HLTV publishes for the same match. This section is the field reference; the full calculation — every formula, constant, table and worked example, plus the current status against HLTV's own Rating 3.0 — is in [RATING](./RATING.MD).

| Field | JSON key | Description |
| --- | --- | --- |
| Value | `value` | The blended rating. 1.00 is an average performance, ~1.30 a very strong map, below ~0.70 a poor one. |
| Kills | `kills` | Eco-adjusted kill points per round, against the baseline. |
| Damage | `damage` | Eco-adjusted damage per round, against the baseline. |
| Survival | `survival` | Eco-weighted rounds survived per round, against the baseline. |
| KAST | `kast` | Eco-weighted rating-KAST rounds per round, against the baseline. See [rating KAST](#rating-kast). |
| MultiKill | `multi_kill` | Multi-kill points per round, against the baseline. |
| RoundSwing | `round_swing` | Round-win-probability swing per round, scaled around 1.00. See [round swing](#round-swing). |

Every sub-rating is normalized so 1.0 is average on that axis, and `value` is their weighted blend: kills 25%, damage 20%, round swing 20%, KAST 15%, survival 10%, multi-kills 10%. HLTV does not publish its weights; these follow the emphasis of their Rating 3.0 announcement. All constants named below live in `cmd/demo_parser/rating.go`.

Like ADR and KAST, the per-round averages divide by the rounds of the whole match, so a player who joined late is diluted on every axis rather than rated on only the rounds they played.

#### Equipment tiers and eco adjustment

Every kill and damage event is priced by the duel's economic matchup. Each player's loadout is bucketed at kill or hurt time into one of six tiers by the strongest weapon they carry: sniper, tier-1 rifle (AK, M4s, AUG, SG 553), tier-2 rifle (FAMAS, Galil, SSG 08), SMG/shotgun/MG, upgraded pistol, starter pistol (Glock, USP-S, P2000). A knife-only player counts as a starter pistol. Per-tier strengths give the expected duel win rate, and points scale inversely with it: an even duel is worth 1.10 kill points — the value HLTV quotes — a tier-1 rifle kill on a starter pistol ~0.54, and the reverse upset ~1.66. Damage is scaled by the same factor. The duel is priced loadout against loadout: a rifler's knife kill is still a rifle-tier duel.

Survival and KAST credit use a per-round weight from the player's own buy, captured when freeze time ends: surviving or staying useful on an eco (weight ~1.5 for a starter pistol) counts for more than doing it with a tier-1 rifle (weight 1), and slightly more than with an AWP (~0.95).

#### Rating KAST

The rating's KAST axis differs from the classic `player_map_stats.kast` in one rule: assists. The classic KAST takes the demo's assist events as they are. The rating additionally credits any player who dealt at least 40 damage to an enemy who died that round — Rating 3.0's threshold, raised from the 25 of Rating 2.0 — whoever or whatever finished them, while keeping the demo's assist events so flash assists still count. Kill, survival and traded-death credit are unchanged. The classic KAST field itself is untouched by this feature.

#### Round swing

Round swing measures how much each kill moved the round towards being won. A win-probability model — a lookup table over both sides' alive counts, with a planted bomb applied as a log-odds shift towards the T side — is evaluated before and after every kill; the killer gains the difference and the victim loses it, so swing sums to zero across each round and, per round, across each 5v5. Diminishing returns are built in: the fifth kill of a wipe moves an almost-decided round far less than an opening kill moves an even one. Post-round kills move nothing. In this first version suicides, teamkills and world deaths move no swing either, and neither does planting or defusing itself — only kills.

The sub-rating is `1 + swing per round × 2.5`, floored at 0. An average player sits at exactly 1.00 by construction, because the swing they gain is swing an opponent lost.

#### Calibration

The per-round baselines that define "average" (0.69 kill points, 70 eco-damage, 0.37 eco-weighted survivals, 0.79 eco-weighted KAST rounds, 0.22 multi-kill points — multi-kill rounds score 1/2/4/7 points for a 2k/3k/4k/5k) were measured over five real matches: the three integration fixtures plus one HLTV pro match and one FACEIT match. Over that set the rounds-weighted mean rating is 1.000 and each sub-rating mean is within 2% of it. The set is small and skews towards standard competitive play; ratings in respawn or wingman modes are computed but not meaningful, since the swing table and the baselines assume 5v5 rounds.

The rating is in JSON, in CSV as `Rating` plus one column per sub-rating, and in the main table as the `Rating` column beside KAST; `--details` prints the full breakdown in the `Rating breakdown` table.

## Match data

The JSON table output also carries match-level data:

| Field | JSON key | Description |
| --- | --- | --- |
| MapName | `map_name` | Map the demo was played on. |
| TotalRounds | `total_rounds` | Rounds played. |
| RoundsWonCT | `rounds_won_ct` | Final score of the side finishing as CT. |
| RoundsWonT | `rounds_won_t` | Final score of the side finishing as T. |
