package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/runner"
	"github.com/tamnd/tamago/pkg/tools"
)

// ExitCode lets main exit non-zero without losing deferred cleanup.
// 0 ok, 1 error (via the returned error), 2 the agent escalated to a human.
var ExitCode int

func runCmd() *cobra.Command {
	var yes, quiet bool
	var maxSteps int
	var outFile string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "run NAME [\"input\"]",
		Short: "run an agent once, input from the arg or stdin",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := garden.Load(args[0])
			if err != nil {
				return err
			}
			var input string
			if len(args) == 2 {
				input = args[1]
			} else {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				input = string(b)
			}
			if strings.TrimSpace(input) == "" {
				return fmt.Errorf("no input: pass it as an argument or pipe it on stdin")
			}
			ctx := cmd.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			rec := &garden.RunRecord{
				Generation: s.Meta.Generation,
				When:       time.Now().UTC().Format(time.RFC3339),
				Input:      input,
			}
			started := time.Now()
			opts := runner.Options{
				MaxSteps: maxSteps,
				Approve:  approver(cmd.ErrOrStderr(), yes),
				OnStep: func(st runner.Step) {
					rec.Steps = append(rec.Steps, garden.RunStep{
						Tool: st.Tool, Args: compactArgs(st.Args),
						Observation: st.Observation, Denied: st.Denied,
					})
					if quiet {
						return
					}
					status := ""
					if st.Denied {
						status = " (denied)"
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "step %d: %s %s -> %d bytes%s\n",
						st.N, st.Tool, compactArgs(st.Args), len(st.Observation), status)
				},
			}
			var onDelta func(string)
			if !quiet && outFile == "" {
				onDelta = func(chunk string) { fmt.Fprint(cmd.OutOrStdout(), chunk) }
			}
			out, err := runner.RunWithOptions(ctx, llm.New(), s, input, onDelta, opts)
			rec.DurationMS = time.Since(started).Milliseconds()
			rec.Output = out
			rec.Escalated = runner.Escalated(out)
			if err != nil {
				rec.Error = err.Error()
			}
			if saveErr := garden.SaveRun(s.Name, rec); saveErr != nil && err == nil {
				err = saveErr
			}
			if err != nil {
				return err
			}
			switch {
			case outFile != "":
				if err := os.WriteFile(outFile, []byte(out+"\n"), 0o644); err != nil {
					return err
				}
				if !quiet {
					fmt.Fprintf(cmd.ErrOrStderr(), "wrote %d bytes to %s\n", len(out)+1, outFile)
				}
			case quiet:
				fmt.Fprintln(cmd.OutOrStdout(), out)
			default:
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if rec.Escalated {
				ExitCode = 2
				fmt.Fprintln(cmd.ErrOrStderr(), "the agent escalated to a human, exit 2")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "approve write and admin tool calls without asking")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "no streaming or step lines, just the final output")
	cmd.Flags().IntVar(&maxSteps, "max-steps", runner.DefaultMaxSteps, "tool call budget for the run")
	cmd.Flags().StringVar(&outFile, "out", "", "write the final output to this file")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "whole-run deadline, like 5m or 90s")
	return cmd
}

// approver asks on the terminal before a write- or admin-floor tool runs.
// Stdin may be the agent's input, so the question goes through /dev/tty;
// no terminal and no --yes means deny.
func approver(errw io.Writer, yes bool) func(name string, a tools.Args) bool {
	if yes {
		return func(string, tools.Args) bool { return true }
	}
	return func(name string, a tools.Args) bool {
		tty, err := os.Open("/dev/tty")
		if err != nil {
			fmt.Fprintf(errw, "cannot ask for approval of %s (no terminal), denying; use --yes for unattended runs\n", name)
			return false
		}
		defer tty.Close()
		fmt.Fprintf(errw, "agent wants to run %s %s [y/N]: ", name, compactArgs(a))
		answer, _ := bufio.NewReader(tty).ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes"
	}
}

// compactArgs renders args on one line, long values elided.
func compactArgs(a tools.Args) string {
	parts := make([]string, 0, len(a))
	for k, v := range a {
		if len(v) > 60 {
			v = v[:60] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, v))
	}
	return "{" + strings.Join(parts, " ") + "}"
}
