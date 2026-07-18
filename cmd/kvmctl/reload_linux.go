//go:build linux

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func reloadSignals(ctx context.Context) <-chan os.Signal {
	channel := make(chan os.Signal, 1)
	signal.Notify(channel, syscall.SIGHUP)
	go func() {
		<-ctx.Done()
		signal.Stop(channel)
	}()
	return channel
}
