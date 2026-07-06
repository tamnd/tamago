// Package cmd wires the tamago CLI.
package cmd

import (
	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/tui"
)

// Root builds the tamago command tree. Bare tamago opens the TUI.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "tamago",
		Short: "agents that write agents",
		Long: `tamago (卵) hatches agents: one line about the job, and it designs the
agent, writes its system prompt, picks its tools inside a risk budget,
births an eval suite, and can grow the agent generation by generation.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run()
		},
	}
	root.AddCommand(newCmd(), genCmd(), runCmd(), evalCmd(), growCmd(), lsCmd(), showCmd())
	return root
}
