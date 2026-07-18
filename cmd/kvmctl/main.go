package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kevin-HYX/kvmctl/internal/app"
	"github.com/Kevin-HYX/kvmctl/internal/cli"
	"github.com/Kevin-HYX/kvmctl/internal/config"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	args := os.Args[1:]
	configPath := config.DefaultPath
	if len(args) >= 2 && args[0] == "--config" {
		configPath = args[1]
		args = args[2:]
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	dependencies := cli.Dependencies{Version: version}
	if commandNeedsController(args) {
		configuration, err := config.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load configuration: %v\n", err)
			return 1
		}
		controller := kvm.NewSwitchableController(app.BuildController(configuration))
		dependencies.Controller = controller
		dependencies.ControlLease = configuration.MCP.ControlLease
		go func(current config.Config) {
			for range reloadSignals(ctx) {
				next, reloadErr := app.ReloadController(configPath, current, controller)
				if reloadErr != nil {
					fmt.Fprintf(os.Stderr, "configuration reload rejected; keeping previous configuration: %v\n", reloadErr)
					continue
				}
				current = next
				fmt.Fprintln(os.Stderr, "configuration reloaded")
			}
		}(configuration)
	}
	return cli.Run(ctx, args, os.Stdout, os.Stderr, dependencies)
}

func commandNeedsController(args []string) bool {
	if len(args) == 0 || args[0] == "version" || args[0] == "config" || args[0] == "devices" {
		return false
	}
	return true
}
