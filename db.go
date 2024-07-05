package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"regexp"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
)

//go:embed migrations
var migrations embed.FS

func openDB(cfg config) (*sqlitemigration.Pool, error) {
	migration, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return nil, errors.Wrap(err, "failed to read migration dir")
	}
	migs := []string{}
	for _, entry := range migration {
		if entry.IsDir() {
			continue
		}
		if !regexp.MustCompile(`^[0-9]{3}_[^\.]+\.sql$`).MatchString(entry.Name()) {
			continue
		}
		contents, err := fs.ReadFile(migrations, filepath.Join("migrations", entry.Name()))
		if err != nil {
			return nil, errors.Wrap(err, "failed to read file")
		}
		migs = append(migs, string(contents))
	}

	schema := sqlitemigration.Schema{
		Migrations: migs,
		AppID:      0x39f796cd,
	}

	pool := sqlitemigration.NewPool(cfg.DatabaseFile, schema, sqlitemigration.Options{
		Flags: sqlite.OpenCreate | sqlite.OpenReadWrite | sqlite.OpenWAL,
		OnError: func(err error) {
			s := spew.NewDefaultConfig()
			s.DisableMethods = true
			slog.Error("migraiton error", slog.String("err", s.Sdump(err)))
		},
		PrepareConn: func(conn *sqlite.Conn) error {
			return conn.CreateFunction("mig_003_backfill", &sqlite.FunctionImpl{
				NArgs: 1,
				Scalar: func(ctx sqlite.Context, args []sqlite.Value) (sqlite.Value, error) {
					if len(args) != 1 {
						return sqlite.Value{}, fmt.Errorf("expected 1 argument, got %d", len(args))
					}
					if arg0type := args[0].Type(); arg0type != sqlite.TypeText {
						return sqlite.Value{}, fmt.Errorf("expected argument 1 to be %s, got %s", sqlite.TypeText, arg0type)
					}
					data, err := decodeOTJSON([]byte(args[0].Text()))
					if err != nil {
						return args[0], nil
					}
					if data.(otLocation).Topic().UnwrapOrZero() != "owntracks/nkcmr/B084C7F9-56B4-490C-8DA3-6D3CB768EA78" {
						slog.Warn("skipping due to topic mismatch")
						return args[0], nil
					}
					if err := enrichOTLocationData(
						context.Background(),
						"nkcmr", "b084c7f9-56b4-490c-8da3-6d3cb768ea78",
						data.(otLocation),
					); err != nil {
						return sqlite.Value{}, err
					}
					return sqlite.TextValue(string(mustJSONEncode(data))), nil
				},
			})
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	conn, err := pool.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("db init failed: %w", err)
	}
	pool.Put(conn)
	return pool, nil
}
