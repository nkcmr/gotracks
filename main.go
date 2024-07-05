package main

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"code.nkcmr.net/gotracks/internal/basicauth"
	"github.com/caarlos0/env/v11"
	"github.com/davecgh/go-spew/spew"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/pkg/errors"
)

type configServer struct {
	Address  string `env:"ADDR" envDefault:":8989"`
	MirrorTo string
}

type config struct {
	DatabaseFile   string       `envDefault:"./db.sqlite3"`
	Username       string       `env:"USERNAME,required"`
	PasswordBcrypt string       `env:"PASSWORD_BCRYPT,required"`
	Server         configServer `envPrefix:"SERVER_"`
}

func _main() error {
	cfg, err := env.ParseAsWithOptions[config](env.Options{
		UseFieldNameByDefault: true,
	})
	if err != nil {
		return errors.Wrap(err, "invalid config")
	}

	dbpool, err := openDB(cfg)
	if err != nil {
		return errors.Wrap(err, "failed to open db")
	}
	defer dbpool.Close()

	if cfg.Username == "" && cfg.PasswordBcrypt == "" {
		return fmt.Errorf("invalid configuration, must add USERNAME and PASSWORD_BCRYPT")
	}

	r := chi.NewRouter()
	mirrorPub(r, cfg)
	r.Use(middleware.Logger)
	r.Use(
		basicauth.Middleware("gotracks", basicauth.InMemoryCredStore{
			cfg.Username: cfg.PasswordBcrypt,
		}),
	)
	r.Use(middleware.Heartbeat("/_healthcheck"))
	r.Use(middleware.Maybe(
		middleware.Timeout(time.Second*5),
		func(r *http.Request) bool {
			return r.Method != "GET" || r.URL.Path != "/ws/last"
		},
	))

	liveLoc := newLiveLocations()

	ListEndpoint(r, dbpool)
	PubEndpoint(r, cfg, liveLoc, dbpool)
	LastLocationEndpoint(r, dbpool)
	LocationsEndpoint(r, dbpool)
	WebsocketLastLocationEndpoint(r, liveLoc, dbpool)

	r.Get("/api/0/version", func(w http.ResponseWriter, r *http.Request) {
		spew.Dump(debug.ReadBuildInfo())
		io.WriteString(w, `{"version":"0.9.7","git":"0.9.7-0-ga865d8da56"}`)
	})

	if err := serveFrontend(r); err != nil {
		return errors.Wrap(err, "failed to serve frontend")
	}

	srv := http.Server{
		Handler: r,
	}

	lis, err := net.Listen("tcp", cfg.Server.Address)
	if err != nil {
		return errors.Wrap(err, "failed to open tcp listener")
	}
	defer lis.Close()
	slog.Info("tcp listener started", slog.String("addr", lis.Addr().String()))

	if err := srv.Serve(lis); err != nil {
		return errors.Wrap(err, "http server failed")
	}
	slog.Info("shutting down...")
	return nil
}

func main() {
	if err := _main(); err != nil {
		defer os.Stderr.Sync()
		fmt.Fprintf(os.Stderr, "%s: error: %s\n", filepath.Base(os.Args[0]), err.Error())
		os.Exit(1)
	}
}

type httpError struct {
	statusCode int
	message    string
}

func (h httpError) Error() string {
	return h.message
}

func (h httpError) StatusCode() int {
	return max(h.statusCode, 400)
}

func badRequest(format string, a ...any) error {
	return httpError{
		statusCode: http.StatusBadRequest,
		message:    fmt.Sprintf(format, a...),
	}
}

func srvError(format string, a ...any) error {
	return httpError{
		statusCode: http.StatusInternalServerError,
		message:    fmt.Sprintf(format, a...),
	}
}

type Point [2]float64

func (p Point) Lat() float64 {
	return p[0]
}
func (p Point) Lon() float64 {
	return p[1]
}

func (p Point) String() string {
	return fmt.Sprintf("%.04f,%.04f", p.Lat(), p.Lon())
}
