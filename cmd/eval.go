package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/evals"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/llm"
)

func evalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "eval NAME",
		Short: "run the agent's eval suite and score it with a judge",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := garden.Load(args[0])
			if err != nil {
				return err
			}
			cl := llm.New()
			rep, err := evals.Run(cmd.Context(), cl, s, func(e evals.Event) {
				switch e.Stage {
				case "run":
					fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] running: %.60s\n", e.Fixture, e.Total, e.Detail)
				case "judge":
					fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] judging\n", e.Fixture, e.Total)
				case "done":
					fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] %s\n", e.Fixture, e.Total, e.Detail)
				}
			})
			if err != nil {
				return err
			}
			if err := evals.Persist(s.Name, rep); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s generation %d: mean %.1f, threshold %.0f\n",
				s.Name, rep.Generation, rep.Mean, evals.PassThreshold)
			for i, f := range rep.Fixtures {
				fmt.Fprintf(cmd.OutOrStdout(), "  %d. %5.1f  %s\n", i+1, f.Score, f.Reason)
			}
			if !rep.Pass {
				fmt.Fprintln(cmd.OutOrStdout(), "FAIL (try tamago grow)")
				return fmt.Errorf("eval below threshold")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "PASS")
			return nil
		},
	}
}
