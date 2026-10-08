// Command town is the ERP Town platform binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/atipongsena/erp-town/go/internal/platform"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("town failed", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: town serve [--roles=a,b]")
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	rolesFlag := fs.String("roles", "", "comma-separated roles (overrides TOWN_ROLES)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	getenv := os.Getenv
	if *rolesFlag != "" {
		getenv = func(k string) string {
			if k == "TOWN_ROLES" {
				return *rolesFlag
			}
			return os.Getenv(k)
		}
	}
	cfg, err := platform.LoadConfig(getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("town serving", "addr", cfg.HTTPAddr)
	return platform.Serve(ctx, cfg.HTTPAddr, platform.NewMux())
}
