# go-test-parser

An advanced research tool for identifying and analyzing unit tests in Go projects.

[![License](https://img.shields.io/github/license/maxgreen01/go-test-parser)](LICENSE)
[![Release](https://img.shields.io/github/v/release/maxgreen01/go-test-parser)](https://github.com/maxgreen01/go-test-parser/releases)

## Features

-   Parse and analyze Go test files.
-   Generate detailed reports on test statistics and analysis.
-   Support for various output formats, including CSV and plaintext.

## Quick Start

Download the latest binary for your operating system from the [Releases](https://github.com/maxgreen01/go-test-parser/releases) page.

### Application Options

Below is a list of the command-line options supported by the application:

| Option              | Description                                                                            | Default Value | Example Argument                               |
| ------------------- | -------------------------------------------------------------------------------------- | ------------- | ---------------------------------------------- |
| `--project` / `-p`  | Path to the Go project directory to be parsed                                          | Required      | `C:/programs/my-go-project`, `./other-project` |
| `--output` / `-o`   | Path to report output file                                                             | Required      | `./output/report.csv`, `stats-report.txt`      |
| `--append`          | Whether to append to the output file instead of overwriting it                         | `false`       | N/a                                            |
| `--splitByDir`      | Whether to parse each top-level directory separately                                   | `false`       | N/a                                            |
| `--threads`         | The number of concurrent threads to use for parsing (only when splitting by directory) | `4`           | `2`, `8`                                       |
| `--logLevel` / `-l` | The minimum severity of log message that should be displayed                           | `info`        | `debug`, `info`, `warn`, `error`               |
| `--timer`           | Whether to print the total execution time of the specified task                        | `false`       | N/a                                            |

To access the help menu and see all available options, run:

```bash
./go-test-parser --help
```

## Commands

### Statistics

The `statistics` command analyzes the Go test files in the specified project directory and generates various statistics related to the project's test cases. This includes metrics such as the total number of test cases, number of test files, average test length,
and the percentage of the project comprised of test code (by lines).

The final results of this command are well-suited for CSV format, especially if using the `splitByDir` option.

Example:

```bash
./go-test-parser statistics --project ./my-go-project --output ./output/statistics-report.csv
```

### Analyze

The `analyze` command performs a deeper analysis of the test cases in a project. This command identifies various structural elements in each test, with a focus on table-driven test indicators. The results of analyzing each test is saved in its own JSON file, which is not controlled by the `output` option.

The final results of this command are well-suited for `.txt` format, as the focal point of this command's output are the JSON files generated for each test case.

Example:

```bash
./go-test-parser analyze --project ./my-go-project --output ./output/analyze-report.txt
```

## Contributing

Contributions are welcome! Please feel free to submit [Issues](https://github.com/maxgreen01/go-test-parser/issues) or [Pull Requests](https://github.com/maxgreen01/go-test-parser/issues)!

## License

This project is licensed under the MIT License. See the [LICENSE](https://github.com/maxgreen01/go-test-parser/blob/main/LICENSE) file for details.
