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
- `-p, --players <players>`: A list of players to analyse. This should be provided as a comma-separated list of player names.
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
