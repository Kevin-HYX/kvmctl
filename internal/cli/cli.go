package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/config"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/Kevin-HYX/kvmctl/internal/mcpserver"
)

// Dependencies contains process-level values and services used by the CLI.
type Dependencies struct {
	Version      string
	Controller   kvm.Controller
	ControlLease time.Duration
}

// Run executes one CLI invocation and returns its process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "kvmctl %s\n", deps.Version)
		return 0
	}
	if len(args) == 2 && args[0] == "status" && args[1] == "--json" {
		if deps.Controller == nil {
			fmt.Fprintln(stderr, "status: controller is not configured")
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(deps.Controller.Status(ctx)); err != nil {
			fmt.Fprintf(stderr, "status: encode response: %v\n", err)
			return 1
		}
		return 0
	}
	if len(args) == 4 && args[0] == "config" && args[1] == "validate" && args[2] == "--file" {
		if _, err := config.Load(args[3]); err != nil {
			fmt.Fprintf(stderr, "configuration is invalid: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "configuration is valid")
		return 0
	}
	if len(args) == 3 && args[0] == "mcp" && args[1] == "--transport" && args[2] == "stdio" {
		if deps.Controller == nil {
			fmt.Fprintln(stderr, "mcp: controller is not configured")
			return 1
		}
		server := mcpserver.NewWithOptions(deps.Version, deps.Controller, mcpserver.Options{ControlLease: deps.ControlLease})
		if err := mcpserver.ServeStdio(ctx, server); err != nil {
			fmt.Fprintf(stderr, "mcp stdio: %v\n", err)
			return 1
		}
		return 0
	}
	if len(args) == 5 && args[0] == "mcp" && args[1] == "--transport" && args[2] == "http" && args[3] == "--listen" {
		if deps.Controller == nil {
			fmt.Fprintln(stderr, "mcp: controller is not configured")
			return 1
		}
		if err := mcpserver.ValidateLoopbackListen(args[4]); err != nil {
			fmt.Fprintf(stderr, "mcp http: %v\n", err)
			return 1
		}
		server := mcpserver.NewWithOptions(deps.Version, deps.Controller, mcpserver.Options{ControlLease: deps.ControlLease})
		if err := mcpserver.ServeHTTP(ctx, args[4], server); err != nil {
			fmt.Fprintf(stderr, "mcp http: %v\n", err)
			return 1
		}
		return 0
	}
	if handled, exitCode := runControllerCommand(ctx, args, stdout, stderr, deps.Controller); handled {
		return exitCode
	}

	fmt.Fprintln(stderr, "usage: kvmctl <version|status|config|mcp|screen|key|text|mouse|release-all>")
	return 2
}
