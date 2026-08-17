# CLI guide

Use `cs2-analyser-tool --help` to list commands or `cs2-analyser-tool <command> --help` for the current command flags.

## Analyse

The `analyse` command parses a CS2 demo file and provides statistical analysis of player performance.

### Command syntax

```sh
cs2-analyser-tool analyse [flags]
```

### Flags

- `-d, --demo <path>`: Path to a CS2 demo file. Repeat the flag in played order to analyse a completed BO3/BO5 series together with `--best-of`.
- `--best-of <n>`: Series format for multiple demos, `3` or `5`. A completed BO3 takes 2 or 3 demos, a completed BO5 takes 3, 4 or 5. Multiple demos always require this flag; the series format is never inferred from the file count, and one demo with it is invalid.
- `-p, --players <players>`: A comma-separated list of player names. Names match case-insensitively, and if any requested name is not in the demo the command fails and lists the available players. In a series, names match any alias the player used across the maps, and a name matching more than one SteamID fails with the candidate SteamIDs.
- `-s, --save`: Save the selected player data.
- `--save-type <type>`: Save as `json` or `csv`. The default is `json`.
- `--details`: Print the extra stat tables that do not fit the main one: the rating breakdown, approximate Rating 3.0 metrics, multi-kill rounds, trades, CT/T side splits, utility effectiveness, and grenades thrown. For a series these show the aggregate values.

### Interactive selection

If `--demo` is omitted, an interactive file picker appears inside the current terminal. It starts in your home directory when the operating system can resolve it, otherwise in the current directory. It does not open a desktop window. Series analysis never starts the picker; every map must be passed explicitly.

If `--players` is omitted, an interactive multiselect appears in the terminal after the demo is parsed. Use the arrow keys or `j`/`k` to move, Space or Enter to toggle a player, and `y` to confirm. Pressing `q` or Ctrl+C closes the selector without a selection; it does not cancel the command, which continues by printing and storing everyone. Series analysis skips the multiselect and analyses everyone unless `--players` is given.

### Analyse one demo

```sh
cs2-analyser-tool analyse --demo path/to/demo/file.dem
```

The command processes the demo, then displays the available players to analyse.

To analyse specific players and save CSV data:

```sh
cs2-analyser-tool analyse --demo path/to/demo/file.dem --players "player1,player2" --save --save-type csv
```

### Analyse a completed BO3/BO5 series

```sh
cs2-analyser-tool analyse --best-of 3 --demo map1.dem --demo map2.dem
cs2-analyser-tool analyse --best-of 3 --demo map1.dem --demo map2.dem --demo map3.dem
cs2-analyser-tool analyse --best-of 5 --demo map1.dem --demo map2.dem --demo map3.dem
```

The demos must be the series' played maps in order. The tool hashes each file (rejecting the same demo supplied twice), parses every map, resolves the two series teams by their rosters, and prints the ordered map results, the overall map and round score, the series winner, and an aggregate player table. Only completed competitive 5v5 series are accepted: Wingman-sized maps are rejected, and the final demo must be the map on which a team clinched, so a 1-1 BO3 or a map supplied after the clinch is an error. With `--save` the JSON contains the full series envelope; see [Player data](./PLAYER_DATA.MD#series-analysis-bo3bo5). CSV stays the flat aggregate-player table with the exact single-map columns, so full per-map series data requires JSON.

### Saved files

Saved JSON is a complete analysis record, not a bare player map:

```json
{
  "players": {
    "76561198000000000": { "team_id": 1, "...": "player data" }
  },
  "teams": [
    {
      "team_id": 1,
      "name": "Rooster",
      "aliases": ["Rooster"],
      "score": 13,
      "roster": [76561198000000000]
    },
    { "team_id": 2, "...": "the other logical team" }
  ],
  "map_data": {
    "map_name": "de_mirage",
    "total_rounds": 24,
    "rounds_won_ct": 11,
    "rounds_won_t": 13
  },
  "game_mode": "premier"
}
```

Read players from `.players`, keyed by SteamID (a JSON string). `map_data`, `teams`, and `game_mode` describe the whole match and always accompany the players; `--players` limits only `.players`. `game_mode` can be `""` when the demo metadata does not expose it, and `rounds_won_ct`/`rounds_won_t` are the final side scores, not team identities. `teams` carries the two logical teams of the map: the lineups that persist through halftime and overtime side switches. Each team has its map-local ID, clan-name aliases, final round wins, and SteamID roster; each player references their team through `team_id`. The details, including how identity is resolved and when parsing fails instead of guessing, are in [Player data](./PLAYER_DATA.MD#logical-teams-teams).

CSV remains a flat player-only table with the same columns as before. This is an intentional breaking change from the previous JSON format, which was the bare `{ "<steam-id>": ... }` map now nested under `players`.

Saved files are created in the current working directory as `<unix-seconds>_data.json` or `<unix-seconds>_data.csv`. The command prints the exact filename after the write succeeds.

The terminal table is narrower than the saved data. See [Player data](./PLAYER_DATA.MD) for every available field. The `Rating` column is an HLTV Rating 3.0-style approximation; the full calculation is in [Rating methodology](./RATING.MD).

## Match history

Every successful analysis is stored automatically in a local SQLite history, including single maps and each played map of a `--best-of` series. No flag is needed; `--save` remains the separate, explicit file export. If storing fails, the analysis already printed remains valid, but the command reports the storage error and exits with status 1.

### Location and contents

The database is `history.db` inside:

- `$CS2_ANALYSER_HISTORY_DIR` when that environment variable is set, otherwise
- `<user config dir>/cs2-analyser-tool/history` (on Linux `~/.config/cs2-analyser-tool/history`, on macOS `~/Library/Application Support/cs2-analyser-tool/history`, on Windows `%AppData%\cs2-analyser-tool\history`).

Everything is local and private: nothing is uploaded, the directory and database are created owner-only where the platform supports it, and the history never stores the original demo bytes or any demo file path. A match is identified by the SHA-256 of the exact demo bytes that were parsed, so re-analysing the same demo from any path or filename never creates a duplicate. The canonical record is kept unchanged and only the player display selection is updated. The stored record is always the complete, unfiltered analysis in the same JSON envelope `--save json` writes; `--players` only chooses who is displayed.

Timestamps in the history are analysis times: when the analysis ran, stored in UTC and displayed in the local time zone. Demos carry no trustworthy record of when the match was played, so no match time is invented.

The schema is versioned with SQLite's `user_version` and every history database is stamped with the tool's `application_id`. A newer database is refused instead of being downgraded. A corrupt file, a database another application put at that path, a stranded WAL or journal beside a missing or empty database file, or any file the tool did not create is also refused. Inspection happens on a private temporary copy, so the refused file, its journal, and its directory keep their exact bytes.

### List history

```sh
cs2-analyser-tool history
```

This lists stored matches newest analysis first: a stable 12-character ID (the digest prefix), the local analysis time, map, score, and game mode. Logical-team scores are preferred; maps without two resolved teams fall back to the final CT/T side scores. An empty history prints a message and exits successfully.

### Show a stored match

```sh
cs2-analyser-tool history show <id>
cs2-analyser-tool history show <id> --details
```

This re-renders a stored match from the database alone; the original demo is not needed and is never opened. `<id>` is the full SHA-256 or any unique prefix of at least 8 hexadecimal characters. An ambiguous prefix fails and lists the candidate IDs. The player selection stored with the match narrows the view exactly as the live rendering did, and the stored record itself always keeps everyone. Without a stored selection everyone is shown, and a series map where none of the selected players appeared keeps its explicit empty view rather than falling back to everyone. `--details` adds the same extra stat tables as `cs2-analyser-tool analyse --details`.

### Compare Premier results

```sh
cs2-analyser-tool compare --player <steam-id-or-name>
```

This shows a player's Premier trend across the stored history: one row per stored map explicitly recorded as `premier` and containing that player, chronologically by analysis time, plus an aggregate totals row. Other modes are never inferred into the trend. A series is stored only as its individual maps, so nothing is counted twice.

`--player` is resolved as a nonzero decimal SteamID64 first. Anything else must match exactly one alias observed in the stored matches, compared case-insensitively under Unicode simple case folding: one-to-one case pairs match (`Ö` finds `ö`, Cyrillic `И` finds `и`), while multi-character expansions do not (`STRASSE` does not find `Straße`). Aliases accumulate across matches, and an alias shared by several SteamIDs fails with each candidate's SteamID and known aliases so you can pass the SteamID64 instead.

Aggregate values are calculated additively from exact stored facts, never by averaging the per-map numbers displayed:

- K/D = sum(kills) / sum(deaths); a deathless run divides by one
- ADR = sum(damage given) / sum(map rounds)
- KAST = 100 × sum(KAST-credited rounds) / sum(map rounds)
- HS% = 100 × sum(headshots) / sum(kills)
- Opening success = 100 × sum(opening rounds won) / sum(opening kills)

Counts (opening duels, trade kills, deaths traded, utility damage, flashes, and grenades thrown) are summed directly. No aggregate rating is fabricated by averaging map ratings.
