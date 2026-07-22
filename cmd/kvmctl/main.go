package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kevin-HYX/kvmctl/internal/app"
	"github.com/Kevin-HYX/kvmctl/internal/cli"
	"github.com/Kevin-HYX/kvmctl/internal/config"
	"github.com/Kevin-HYX/kvmctl/internal/control"
	"github.com/Kevin-HYX/kvmctl/internal/kvm"
	"github.com/Kevin-HYX/kvmctl/internal/mcpserver"
	"github.com/Kevin-HYX/kvmctl/internal/webcontrol"
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
		if len(args) > 0 && args[0] == "serve" {
			if configuration.Server.PublicOrigin == "" {
				fmt.Fprintln(os.Stderr, "serve: server.public_origin is required")
				return 1
			}
			releaseInput := func() { _ = controller.ReleaseAll(context.Background()) }
			arbiter := control.New(configuration.MCP.ControlLease, 5*time.Second, releaseInput)
			sessions := webcontrol.NewSessionManager(webcontrol.SessionOptions{
				PublicOrigin:   configuration.Server.PublicOrigin,
				EntryTTL:       10 * time.Minute,
				ReconnectGrace: time.Minute,
				OnSessionEnd:   releaseInput,
			})
			browserServer, err := webcontrol.NewServer(webcontrol.ServerOptions{
				PublicOrigin:   configuration.Server.PublicOrigin,
				Sessions:       sessions,
				Control:        arbiter,
				Controller:     controller,
				FrameRate:      30,
				WebRTCEnabled:  configuration.Server.WebRTC.Enabled,
				ICEServers:     configuration.Server.WebRTC.STUNURLs,
				ConnectTimeout: configuration.Server.WebRTC.ConnectTimeout,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "serve: configure browser control: %v\n", err)
				return 1
			}
			mcpServer := mcpserver.NewWithOptions(version, controller, mcpserver.Options{
				ControlLease:    configuration.MCP.ControlLease,
				Control:         arbiter,
				BrowserSessions: sessions,
			})
			mux := http.NewServeMux()
			mux.Handle("/mcp", mcpserver.NewHTTPHandler(mcpServer))
			mux.Handle("/control/", browserServer.Handler())
			dependencies.HTTPHandler = mux
		}
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
