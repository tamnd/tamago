package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/llm"
	"github.com/tamnd/tamago/pkg/runner"
)

func runCmd() *cobra.Command {
	return &cobra.Command{
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
			_, err = runner.Run(cmd.Context(), cl, s, input, func(chunk string) {
				fmt.Fprint(cmd.OutOrStdout(), chunk)
			})
			fmt.Fprintln(cmd.OutOrStdout())
			return err
		},
	}
}
