package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

func main() {
	root := &cobra.Command{
		Use:   "mm",
		Short: "Interactive Mihomo Manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunInteractive()
		},
	}

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
