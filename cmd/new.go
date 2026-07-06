package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/designer"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/llm"
)

func newCmd() *cobra.Command {
	var name, from string
	c := &cobra.Command{
		Use:   "new \"job description\"",
		Short: "design and hatch a new agent from a one-line job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := designer.Options{Name: name}
			if from != "" {
				parent, err := garden.Load(from)
				if err != nil {
					return err
				}
				opts.Parent = parent
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "designing agent...")
			cl := llm.New()
			dots := 0
			s, err := designer.Design(cmd.Context(), cl, args[0], opts, func(string) {
				dots++
				if dots%20 == 0 {
					fmt.Fprint(cmd.ErrOrStderr(), ".")
				}
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr())
			if err := garden.Save(s); err != nil {
				return err
			}
			dir, _ := garden.Dir(s.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "hatched %s (risk %s, tier %s, %d eval fixtures)\n", s.Name, s.Risk, s.Tier, len(s.Evals))
			fmt.Fprintf(cmd.OutOrStdout(), "spec: %s/spec.yaml\n", dir)
			fmt.Fprintf(cmd.OutOrStdout(), "next: tamago eval %s, then tamago gen %s\n", s.Name, s.Name)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "override the agent name")
	c.Flags().StringVar(&from, "from", "", "design a variant of an existing agent")
	return c
}
