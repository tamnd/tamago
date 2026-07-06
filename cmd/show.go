package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/garden"
	"gopkg.in/yaml.v3"
)

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show NAME",
		Short: "show an agent's spec, lineage, and eval history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := garden.Load(args[0])
			if err != nil {
				return err
			}
			b, err := yaml.Marshal(s)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), string(b))
			if s.Meta.Parent != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\nlineage: descended from %s\n", s.Meta.Parent)
			}
			hist, err := garden.EvalHistory(s.Name)
			if err == nil && len(hist) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\neval history:")
				for _, r := range hist {
					verdict := "fail"
					if r.Pass {
						verdict = "pass"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  gen %d  %s  mean %.1f  %s\n", r.Generation, r.When, r.Mean, verdict)
				}
			}
			gens, err := garden.Generations(s.Name)
			if err == nil && len(gens) > 1 {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%d generations on file\n", len(gens))
			}
			return nil
		},
	}
}
