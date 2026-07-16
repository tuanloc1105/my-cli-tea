package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"common-module/utils"
	"github.com/spf13/cobra"
)

type conversionRunner func(context.Context, string, commandOptions, io.Writer) error
type clearScreenFunc func()

type commandOptions struct {
	file   string
	all    bool
	format string
}

// ExecuteContext runs case-converter with the supplied process arguments and
// streams. It returns the process exit code without terminating the caller.
func ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeContext(ctx, args, stdout, stderr, runConversions, utils.CLS)
}

func executeContext(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	run conversionRunner,
	clearScreen clearScreenFunc,
) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if args == nil {
		args = []string{}
	}

	root := newRootCommand(run, clearScreen, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(
	run conversionRunner,
	clearScreen clearScreenFunc,
	stdout io.Writer,
	stderr io.Writer,
) *cobra.Command {
	options := commandOptions{}

	root := &cobra.Command{
		Use:   "case-converter",
		Short: "Case Converter CLI Tool - A text case conversion utility",
		Long: `Case Converter CLI Tool - A command-line tool for text case conversion and transformation.

Examples:
  # Convert text to various cases
  case-converter "hello world"

  # Convert from file
  case-converter -f input.txt

  # Show all case conversions
  case-converter "hello world" --all

  # Output specific format only
  case-converter "hello world" --format snake`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommand(cmd.Context(), cmd, args, options, run, clearScreen, stdout)
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.Flags()
	flags.StringVarP(&options.file, "file", "f", "", "Input file containing text to convert")
	flags.BoolVar(&options.all, "all", false, "Show all case conversions")
	flags.StringVar(&options.format, "format", "", "Specific format to output (normal, upper, lower, snake, kebab, camel, pascal, constant, title, dot, path)")

	return root
}

func runCommand(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	options commandOptions,
	run conversionRunner,
	clearScreen clearScreenFunc,
	stdout io.Writer,
) error {
	if clearScreen != nil {
		clearScreen()
	}

	var inputText string
	if options.file != "" {
		content, err := os.ReadFile(options.file)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
		inputText = string(content)
	} else if len(args) > 0 {
		inputText = args[0]
	} else {
		return cmd.Help()
	}

	if run == nil {
		return errors.New("conversion runner is nil")
	}
	return run(ctx, inputText, options, stdout)
}
