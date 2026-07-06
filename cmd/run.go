package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/runner"
	"github.com/tamnd/tamago/pkg/tools"
)

func runCmd() *cobra.Command {
	var yes bool
	var maxSteps int
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
			cl := llm.New()
			opts := runner.Options{
				MaxSteps: maxSteps,
				Approve:  approver(cmd.ErrOrStderr(), yes),
				OnStep: func(st runner.Step) {
					status := ""
					if st.Denied {
						status = " (denied)"
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "step %d: %s %s -> %d bytes%s\n",
						st.N, st.Tool, compactArgs(st.Args), len(st.Observation), status)
				},
			}
			_, err = runner.RunWithOptions(cmd.Context(), cl, s, input, func(chunk string) {
				fmt.Fprint(cmd.OutOrStdout(), chunk)
			}, opts)
			fmt.Fprintln(cmd.OutOrStdout())
			return err
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "approve write and admin tool calls without asking")
	cmd.Flags().IntVar(&maxSteps, "max-steps", runner.DefaultMaxSteps, "tool call budget for the run")
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
