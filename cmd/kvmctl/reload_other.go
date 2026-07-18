//go:build !linux

package main

import (
	"context"
	"os"
)

func reloadSignals(context.Context) <-chan os.Signal {
	return make(chan os.Signal)
}
