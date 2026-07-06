package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/evals"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/grow"
	"github.com/tamnd/tamago/pkg/llm"
)

func growCmd() *cobra.Command {
	var rounds int
	c := &cobra.Command{
		Use:   "grow NAME",
		Short: "improve an agent: eval, rewrite, keep the better generation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := garden.Load(args[0])
			if err != nil {
				return err
			}
			cl := llm.New()
			results, err := grow.Grow(cmd.Context(), cl, s, rounds,
				func(e grow.Event) {
					fmt.Fprintf(cmd.ErrOrStderr(), "round %d %s: %s\n", e.Round, e.Stage, e.Detail)
				},
				func(e evals.Event) {
					if e.Stage == "done" {
						fmt.Fprintf(cmd.ErrOrStderr(), "  fixture %d/%d: %s\n", e.Fixture, e.Total, e.Detail)
					}
				})
			if err != nil {
				return err
			}
			for _, r := range results {
				verdict := "dropped"
				if r.Kept {
					verdict = fmt.Sprintf("kept as generation %d", r.Generation)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "round %d: %.1f -> %.1f, %s\n", r.Round, r.Before, r.After, verdict)
			}
			return nil
		},
	}
	c.Flags().IntVar(&rounds, "rounds", 1, "improvement rounds")
	return c
}
