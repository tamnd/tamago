package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/tamago/pkg/garden"
	"github.com/tamnd/tamago/pkg/gen"
)

func genCmd() *cobra.Command {
	var target, out string
	c := &cobra.Command{
		Use:   "gen NAME",
		Short: "generate runnable artifacts from an agent spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := garden.Load(args[0])
			if err != nil {
				return err
			}
			targets := gen.All()
			if target != "" {
				t, err := gen.ByName(target)
				if err != nil {
					return err
				}
				targets = []gen.Target{t}
			}
			if out == "" {
				out = s.Name + "-agent"
			}
			wrote := 0
			for _, t := range targets {
				files, err := t.Generate(s)
				if err != nil {
					return fmt.Errorf("target %s: %w", t.Name(), err)
				}
				for rel, content := range files {
					path := filepath.Join(out, rel)
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						return err
					}
					mode := os.FileMode(0o644)
					if strings.HasSuffix(rel, ".sh") {
						mode = 0o755
					}
					if err := os.WriteFile(path, []byte(content), mode); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
					wrote++
				}
			}
			if wrote == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to generate for this spec and target")
			}
			return nil
		},
	}
	c.Flags().StringVar(&target, "target", "", "one target: claude, standalone, cron (default all)")
	c.Flags().StringVar(&out, "out", "", "output directory (default NAME-agent)")
	return c
}
