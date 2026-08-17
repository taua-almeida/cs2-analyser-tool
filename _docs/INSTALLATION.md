# Installation

## Prebuilt archives

Prebuilt archives are published for each release from `v0.1.0` onward at [GitHub Releases](https://github.com/taua-almeida/cs2-analyser-tool/releases). Download `checksums.txt` and the archive for your system from the same release:

| System | Archive |
| --- | --- |
| Linux, 64-bit Intel/AMD | `cs2-analyser-tool-linux-amd64.tar.gz` |
| Windows, 64-bit Intel/AMD | `cs2-analyser-tool-windows-amd64.zip` |
| macOS, Intel | `cs2-analyser-tool-darwin-amd64.tar.gz` |
| macOS, Apple silicon | `cs2-analyser-tool-darwin-arm64.tar.gz` |

Verify the downloaded archive before extracting it. On Linux, replace the archive name if needed:

```sh
grep 'cs2-analyser-tool-linux-amd64.tar.gz$' checksums.txt | sha256sum --check -
```

On macOS:

```sh
grep 'cs2-analyser-tool-darwin-arm64.tar.gz$' checksums.txt | shasum -a 256 --check -
```

On Windows PowerShell:

```powershell
$archive = "cs2-analyser-tool-windows-amd64.zip"
$expected = ((Get-Content checksums.txt | Where-Object { $_ -match ([regex]::Escape($archive) + '$') }) -split '\s+')[0]
$actual = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "checksum mismatch" }
```

Extract a Linux or macOS archive into its own directory and run the binary:

```sh
mkdir cs2-analyser-tool-release
tar -xzf cs2-analyser-tool-linux-amd64.tar.gz -C cs2-analyser-tool-release
./cs2-analyser-tool-release/cs2-analyser-tool version
```

For Windows:

```powershell
Expand-Archive cs2-analyser-tool-windows-amd64.zip -DestinationPath cs2-analyser-tool-release
.\cs2-analyser-tool-release\cs2-analyser-tool.exe version
```

Each archive also contains `README.md`, `LICENSE`, and the linked `_docs` reference files.

## Install with Go

Install the latest available CLI version with a supported Go toolchain. Ensure the Go install directory is on `PATH`: Go uses `GOBIN` when set, otherwise the `bin` directory under `GOPATH`.

```sh
go install github.com/taua-almeida/cs2-analyser-tool@latest
cs2-analyser-tool version
```

`@latest` resolves a pseudo-version of `main` until the first tag is published; after that it selects the latest tagged release.

Continue with the [five-minute quick start](../README.md#five-minute-quick-start) or the full [CLI guide](./CLI.md).
