package main

import (
	"context"
	"os"

	"charm.land/fang/v2"

	"github.com/tamnd/tamago/cmd"
)

func main() {
	if err := fang.Execute(context.Background(), cmd.Root()); err != nil {
		os.Exit(1)
	}
}
