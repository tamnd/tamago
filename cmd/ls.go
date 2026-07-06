package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/garden"
)

func lsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "list agents in the garden",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := garden.List()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "the garden is empty, hatch one: tamago new \"job description\"")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tRISK\tTIER\tGEN\tSCORE\tJOB")
			for _, e := range entries {
				score := "-"
				if e.LastScore >= 0 {
					score = fmt.Sprintf("%.1f", e.LastScore)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%.50s\n",
					e.Spec.Name, e.Spec.Risk, e.Spec.Tier, e.Spec.Meta.Generation, score, e.Spec.Job)
			}
			return w.Flush()
		},
	}
}
