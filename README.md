# go-test-parser

An advanced research tool for identifying and analyzing unit tests in Go projects.

[![License](https://img.shields.io/github/license/maxgreen01/go-test-parser)](LICENSE)
[![Release](https://img.shields.io/github/v/release/maxgreen01/go-test-parser)](https://github.com/maxgreen01/go-test-parser/releases)

## Features

-   Parse and analyze Go test files.
-   Generate detailed reports on test statistics and analysis.
-   Support for various output formats, including CSV and plaintext.
-   Structured logging multiplexed to the terminal and `testparser.log`.

## Quick Start

Download the latest binary for your operating system from the [Releases](https://github.com/maxgreen01/go-test-parser/releases) page.

To start the program, run it in the command line using the following format:

```bash
./go-test-parser <command> [options]
```

For a list of supported commands, see the [Commands](#commands) section. For a list of available options, see the [Application Options](#application-options) section, as well as the Command Options subsection for each command.

### Application Options

Below is a list of the command-line options supported by the application:

| Option              | Description                                                                            | Default Value | Example Argument                               |
| ------------------- | -------------------------------------------------------------------------------------- | ------------- | ---------------------------------------------- |
| `--project` / `-p`  | Path to the Go project directory to be parsed                                          | Required      | `C:/programs/my-go-project`, `./other-project` |
| `--output` / `-o`   | Path to report output file                                                             | Required      | `./output/report.csv`, `stats-report.txt`      |
| `--append`          | Whether to append to the output file instead of overwriting it                         | `false`       | N/a                                            |
| `--splitByDir`      | Whether to parse each top-level directory separately                                   | `false`       | N/a                                            |
| `--threads`         | The number of concurrent threads to use for parsing (only when splitting by directory) | `4`           | `2`, `8`                                       |
| `--logLevel` / `-l` | The minimum severity of log message that should be displayed                           | `info`        | `debug`, `info`, `warn`, `error` (exhaustive)  |
| `--timer`           | Whether to print the total execution time of the specified task                        | `false`       | N/a                                            |

To access the help menu and see all available options, run:

```bash
./go-test-parser --help
```

To view the command-specific

## Commands

### Statistics

The `statistics` command analyzes the Go test files in the specified project directory and generates various statistics related to the project's test cases. This includes metrics such as the total number of test cases, number of test files, average test length, and the percentage of the project comprised of test code (by lines).

Supports output to either `.txt` or `.csv` files. Output is especially well-suited for a `.csv` file if using the `splitByDir` option.

Example:

```bash
./go-test-parser statistics --project ./my-go-project --output ./output/statistics-report.csv
```

### Analyze

The `analyze` command performs a deeper analysis of the test cases in a project. This command identifies various structural elements in each test, with a focus on table-driven tests. The results of analyzing the tests are saved in their own JSON files, which are put in a new folder in the same directory as the `output` file. The JSON files are named like `<project>/<project>_<package>_<testName>.json`.

Certain detected test cases can also be refactored using the `refactor` option, as described in the [Command Options](#analyze-command-options) subsection.

Supports output to either `.txt` or `.csv` files. Output is especially well-suited for a `.csv` file because it will contain a condensed version of the analysis results of every test case.

Example:

```bash
./go-test-parser analyze --project ./my-go-project --output ./output/analyze-report.csv
```

#### Analyze Command Options

The following command-line options are only supported by the `analyze` command.

| Option       | Description                                                                                     | Default Value | Example Argument               |
| ------------ | ----------------------------------------------------------------------------------------------- | ------------- | ------------------------------ |
| `--refactor` | The type of refactoring to perform on the detected test cases. See below for additional details | `none`        | `none`, `subtest` (exhaustive) |

The `refactor` command indicates which type of refactoring should be performed on certain detected test cases. Refactoring does not modify the original source files, but instead saves the entire refactored function as a field in the JSON output file for each affected test case. If the `none` argument is specified, no refactoring will be performed.

The `subtest` refactoring method affects tests that are detected to be table-driven but do not use `t.Run()` to declare subtests. The refactoring wraps the entire contents of the execution loop in a `t.Run()` call, using the detected scenario name field (or a stringified version of one of the input fields) as the subtest name.

## Contributing

Contributions are welcome! Please feel free to submit [Issues](https://github.com/maxgreen01/go-test-parser/issues) or [Pull Requests](https://github.com/maxgreen01/go-test-parser/issues)!

## License

This project is licensed under the MIT License. See the [LICENSE](https://github.com/maxgreen01/go-test-parser/blob/main/LICENSE) file for details.
