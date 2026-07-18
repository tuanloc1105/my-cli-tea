# Cobra CLI Conventions

Use this contract for all five standalone CLI modules. `replace-text` is the
reference implementation. Keep each module independent; `common-module` must
not depend on Cobra and must not contain a shared command factory.

## Lifecycle

Every process follows the same call chain:

```text
main -> ExecuteContext -> newRootCommand -> RunE -> injected runner/domain logic
```

`main.go` is only a process adapter:

```go
func main() {
	os.Exit(cmd.ExecuteContext(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
```

Each `cmd` package follows these rules:

- Export `ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int`.
- Replace a nil context with `context.Background()`, nil writers with
  `io.Discard`, and a nil args slice with an explicit empty slice so Cobra does
  not fall back to the host process's `os.Args`.
- Construct a new root command for every call, bind flags to a new local
  `commandOptions` value, set the supplied args and streams, then call
  `ExecuteContext` on the command.
- Set `SilenceErrors` and `SilenceUsage` on the root command.
- Use `RunE`; return validation and operational errors instead of printing an
  error or terminating the process from a command callback.
- Render a returned error exactly once as `Error: <message>` on stderr and
  return exit code 1. Return 0 for success and help.
- Keep `os.Exit` in `main` only. Command and domain packages must remain safe to
  invoke repeatedly in one process.

The command constructor may be unexported. Inject the narrowest runner and
side-effect functions needed for deterministic tests; avoid a repository-wide
CLI abstraction.

## Streams And Context

- Set both Cobra writers with `SetOut` and `SetErr`.
- Write command output through the supplied writer or `cmd.OutOrStdout()` and
  `cmd.ErrOrStderr()`. Do not write to `os.Stdout` or `os.Stderr` from a Cobra
  action when an injected stream is available.
- Pass the command context into runners and derive timeouts, cancellation, and
  signal-aware work from it without replacing it with a background context.
- Inject terminal-only effects such as screen clearing and interactive prompt
  streams so tests do not mutate the caller's terminal.
- Preserve the existing logical destination and exact successful output,
  including ANSI sequences and JSON formatting.

## Arguments, Flags, And Errors

Preserve every existing flag name, shorthand, default, help string, accepted
positional-argument policy, precedence rule, and domain fallback. Lifecycle
normalization does not authorize stricter validation or a CLI redesign.

Unless a module-specific contract below documents distinct status codes, wrong
arguments, unknown flags, validation failures, and operational failures return
exit code 1 and one error line on stderr. They must not duplicate the error on
stdout or print usage unless help was explicitly requested.

## Module Compatibility

| Module | Behavior that must remain unchanged |
| --- | --- |
| `find-content` | Search accepts exactly `<directory> <keyword>`; list accepts canonical `--list <directory>` plus the deprecated two-argument form. Preserve deterministic relative-path ordering, exact nonnegative `--max-results`, CSV normalization, hidden/default-exclude policy, regular-file/NUL checks, and exit `0` match/list/help, `1` clean no-match, `2` usage/fatal/partial-error behavior. |
| `check-folder-size` | Keep `cobra.MaximumNArgs(1)`, default path, size parsing, sorting, filtering, timeout, JSON formatting, progress text, and screen-clear policy. |
| `find-everything` | Keep `cobra.ExactArgs(2)`, basename `*`/`?` matching, validated finite size bounds, exact combined result caps, large-result conflict/prompt handling, exit codes `0/1/2/130`, stdout/stderr separation, and ANSI/progress only on the corresponding TTY stream. The interactive large-result prompt reads `s` or `d` as a single key without Enter and must restore terminal state; non-TTY input falls back to saving without prompting. |
| `api-stress-test` | Continue accepting arbitrary positional arguments; retain existing flag defaults plus the optional `--shutdown-grace`, non-positive request/concurrency fallbacks, and stdout/stderr separation. Preserve schema-v2 reports, strict pacing across warmup/measurement, graceful drain with planned-cancellation accounting, TTY-only progress, atomic JSON report replacement, failure exit `1`, parent/SIGINT `130`, and SIGTERM `143`. |
| `replace-text` | Keep `cobra.ExactArgs(3)`, reporter behavior, escape handling, filesystem safety policies, and the existing exit contract. |

Known product quirks listed above are compatibility requirements for this
lifecycle work. Fix them only under a separate approved change.

For `find-everything` prompt tests, do not rely only on a finite
`strings.Reader("d")`: EOF lets line-oriented scanners return a token without a
newline and can hide an Enter-required regression. Use a reader that fails on a
second read, then perform a real TTY smoke test after changing raw-mode setup or
restoration.

## Command Tests

Command tests should call `ExecuteContext` or its injected internal helper with
a fresh context, args, stdout buffer, stderr buffer, runner, and side-effect
functions. Cover at least:

- help, argument validation, unknown flags, defaults, and flag forwarding;
- precedence rules and representative successful output;
- operational failures, exit codes, and one-time stderr rendering;
- nil context and writer handling where the internal contract is exercised;
- two sequential invocations with different flags to prove option state does
  not leak between commands;
- context cancellation and stream routing where the command starts work;
- deterministic fixtures for filesystem behavior without depending on
  concurrent result ordering.

For each module, run focused command tests first, then `go test ./...`,
`go vet ./...`, and a build to `/tmp`. Run Go commands from the module directory
because the repository has no top-level `go.mod`.
