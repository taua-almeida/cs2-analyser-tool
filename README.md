# cs2-analyser-tool
Designed specifically for players and coaches, this command-line interface tool provides a simple display and easy data ready to analyse, compare and start your journey towards improving your team and your personal CS2 skills.

## Usage

### Analyse

This command parses a CS2 demo file and provides statistical analysis of players' performance.

#### Command Syntax

```bash
analyse [flags]
```

#### Flags

- `-d, --demo <path>`: Path to the CS2 demo file.
- `-p, --players <players>`: A list of players to analyse. This should be provided as a comma-separated list of player names. Names match case-insensitively, and if any requested name is not in the demo the command fails and lists the available players.
- `-s, --save`: Flag to save the demo player's data.
- `--save-type <type>`: Type of file to save the data. Options are `json` and `csv`. The default is `json`.
- `--details`: Print the extra stat tables that do not fit the main one: the rating breakdown, approximate Rating 3.0 metrics, multi-kill rounds, trades, CT/T side splits, utility effectiveness and grenades thrown.

#### Options

- `d`: If -d not provided, the CLI will pop a window to select the file in our system.
- `p`: If no players are provided, a multiselect option will show on terminal.

#### Examples

**Analyzing a Specific Demo**

```bash
analyse --demo path/to/demo/file
```
This command will process the specified demo file and output the analysis to the console and will display all available players to analyse.

**Analyzing Specific Players and Saving the Data**

```bash
analyse --demo path/to/demo --players "player1,player2" --save --save-type csv
```

This will analyse only "player1" and "player2" from the specified demo and save the data in CSV format.

#### Saved files

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

Read players from `.players`, keyed by SteamID (a JSON string). `map_data`, `teams` and `game_mode` describe the whole match and always accompany the players; `--players` limits only `.players`. `game_mode` can be `""` when the demo metadata does not expose it, and `rounds_won_ct`/`rounds_won_t` are the final side scores, not team identities. `teams` carries the two logical teams of the map — the lineups that persist through halftime and overtime side switches — with each team's map-local ID, clan-name aliases, final round wins and SteamID roster; each player references their team through `team_id`. The details, including how identity is resolved and when parsing fails instead of guessing, are in [PLAYER_DATA](./_docs/PLAYER_DATA.MD#logical-teams-teams). CSV remains a flat player-only table with the same columns as before. This is an intentional breaking change from the previous format, which was the bare `{ "<steam-id>": ... }` map now nested under `players`.

#### Analyzed data

The data output showed in the terminal table is not all the analyzed data, to get more info about the available data, go to [PLAYER_DATA](./_docs/PLAYER_DATA.MD). The `Rating` column is an HLTV Rating 3.0-style approximation; how it is calculated, constant by constant, is documented in [RATING](./_docs/RATING.MD).

## Contributing

The opt-in external-oracle test for HLTV match 129241 is documented in
[HLTV_REGRESSION.md](./_docs/HLTV_REGRESSION.md). It is separate from the
repository's self-golden integration fixtures and never downloads demos or
scrapes HLTV during a test run.

### Clone the repo

```bash
git clone https://github.com/taua-almeida/cs2-analyser-tool.git
cd cs2-analyser-tool
```

### Build the project

```bash
go build
```

### Submit a pull request

If you'd like to contribute, please fork the repository and open a pull request to the `main` branch.
