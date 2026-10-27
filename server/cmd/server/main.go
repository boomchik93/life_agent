package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // zone names in feeds must resolve even without system tzdata

	"lifeagent/internal/api"
	"lifeagent/internal/calendar"
	"lifeagent/internal/config"
	"lifeagent/internal/store"
)

const backupKeep = 14 * 24 * time.Hour

func main() {
	envFile := flag.String("env", "", "optional KEY=VALUE file loaded for unset variables")
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(*envFile, flag.Arg(0), logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(envFile, cmd string, logger *slog.Logger) error {
	if envFile != "" {
		if err := config.LoadDotEnv(envFile); err != nil {
			return err
		}
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	switch cmd {
	case "", "serve":
		return serve(cfg, st, logger)
	case "backup":
		path, err := st.Backup(cfg.BackupDir, backupKeep, time.Now())
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	default:
		return fmt.Errorf("unknown command %q (want serve or backup)", cmd)
	}
}

// buildCalendars wires the configured calendar sources; it returns nil when none is set.
func buildCalendars(ctx context.Context, cfg config.Config, logger *slog.Logger) (*calendar.Aggregator, error) {
	agg := &calendar.Aggregator{}
	if len(cfg.CalendarIDs) > 0 {
		key, err := os.ReadFile(cfg.GoogleKeyFile)
		if err != nil {
			return nil, fmt.Errorf("read GOOGLE_SA_KEY_FILE: %w", err)
		}
		// One source per calendar: a failing calendar becomes a warning
		// instead of silently disappearing behind the others' events.
		for _, id := range cfg.CalendarIDs {
			g, err := calendar.NewGoogle(ctx, key, []string{id}, cfg.Location)
			if err != nil {
				return nil, err
			}
			agg.Sources = append(agg.Sources, calendar.Named{Name: "google", Provider: g})
		}
	}
	if cfg.ICSURL != "" {
		agg.Sources = append(agg.Sources, calendar.Named{Name: "ics", Provider: calendar.NewICS(cfg.ICSURL, "Расписание", cfg.Location)})
	}
	if len(agg.Sources) == 0 {
		logger.Warn("no calendar sources configured")
		return nil, nil
	}
	return agg, nil
}

func serve(cfg config.Config, st *store.Store, logger *slog.Logger) error {
	var collector api.Collector
	agg, err := buildCalendars(context.Background(), cfg, logger)
	if err != nil {
		return err
	}
	if agg != nil {
		collector = agg
	}

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: api.New(api.Options{
			Store: st, Token: cfg.Token, Logger: logger,
			Calendar: collector, Location: cfg.Location,
			RatePerSec: 5, Burst: 20,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("listening", "addr", cfg.Listen)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
