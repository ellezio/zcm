package main

import "errors"

func parseCLIArgs(args []string) (*cli, error) {
	cli := newCLI()

	argsLen := len(args)
	for i := 0; i < argsLen; i++ {

		switch args[i] {
		case "--targets-file", "-t":
			i++
			var path string
			if i < argsLen && args[i][:1] != "-" {
				path = args[i]
			}

			if path == "" {
				return nil, errors.New("invalid argument for \"--targets-file\"")
			}

			cli.targetsFile = path
		}
	}

	return cli, nil
}

func newCLI() *cli {
	cli := &cli{}

	cli.targetsFile = "monitoring-targets.yml"

	return cli
}

type cli struct {
	targetsFile string
}
