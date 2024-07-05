package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"code.nkcmr.net/gotracks/internal/basicauth"
	"code.nkcmr.net/gotracks/internal/ep"
	"code.nkcmr.net/opt"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	olc "github.com/google/open-location-code/go"
	"github.com/mmcloughlin/geohash"
	"github.com/pkg/errors"
	"github.com/ugjka/go-tz/v2"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

type PubRequest struct {
	User   string `query:"q" header:"x-limit-u"`
	Device string `query:"d" header:"x-limit-d"`
	Body   []byte
}

type PubResponse struct {
	Messages []map[string]any
}

func (p PubResponse) APIResponse() any {
	return p.Messages
}

func enrichOTLocationData(ctx context.Context, user, device string, otdata otLocation) error {
	p, ok := otdata.LatLng().MaybeUnwrap()
	if !ok {
		return badRequest("lat,lon missing")
	}
	otdata["ghash"] = geohash.EncodeWithPrecision(p.Lat(), p.Lon(), 7)
	otdata["pluscode"] = olc.Encode(p.Lat(), p.Lon(), 12)
	tzs, err := tz.GetZone(tz.Point{Lat: p.Lat(), Lon: p.Lon()})
	if err != nil {
		return errors.Wrap(err, "failed to determine time zone from location")
	}
	if len(tzs) > 0 {
		otdata["tzname"] = tzs[0]
		loc, err := time.LoadLocation(tzs[0])
		if err != nil {
			return errors.Wrap(err, "unable to load timezone from stdlib")
		}
		tst, ok := otdata.Timestamp().MaybeUnwrap()
		if !ok {
			return badRequest("missing tst timestamp")
		}
		otdata["isolocal"] = tst.In(loc).Format(time.RFC3339)
		otdata["disptst"] = tst.UTC().Format("2006-01-02 03:04:05")
		otdata["isotst"] = tst.UTC().Format(time.RFC3339)
	}
	otdata["username"] = user
	otdata["device"] = device
	otdata["_http"] = true
	if t, ok := otdata.Topic().MaybeUnwrap(); ok && t != fmt.Sprintf("owntracks/%s/%s", user, strings.ToUpper(device)) {
		slog.WarnContext(ctx, "unexpected topic", slog.String("input_topic", t))
	}
	return nil
}

func checkOutbox(ctx context.Context, conn *sqlite.Conn, user, device string) ([]map[string]any, error) {
	var lastIdx int64
	var lastIdxSet atomic.Bool
	const lastIdxQuery = `
		SELECT last_outbox_id
		FROM cmd_outbox_consumer_idx
		WHERE user = ?1 AND device = ?2
	`
	err := sqlitex.Execute(conn, lastIdxQuery, &sqlitex.ExecOptions{
		Args: []any{user, device},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			if lastIdxSet.CompareAndSwap(false, true) {
				lastIdx = stmt.ColumnInt64(0)
				return nil
			}
			return fmt.Errorf("expected only 1 result")
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to query last consumed index")
	}

	const getOutboxQuery = `
		SELECT id, data
		FROM cmd_outbox
		WHERE id > ?1 
			AND user = ?2 
			AND device IN (?3, '*') 
			AND COALESCE(when_expires, (1 << 62)) > strftime('%s', 'now')
	`
	var items []map[string]any
	var maxItem int
	err = sqlitex.Execute(conn, getOutboxQuery, &sqlitex.ExecOptions{
		Args: []any{lastIdx, user, device},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			maxItem = max(maxItem, stmt.ColumnInt(0))
			var item map[string]any
			if err := json.Unmarshal([]byte(stmt.ColumnText(1)), &item); err != nil {
				return errors.Wrap(err, "corrupt outbox item")
			}
			items = append(items, item)
			return nil
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get outbox items")
	}

	queries := []string{
		`INSERT OR IGNORE INTO cmd_outbox_consumer_idx (user, device, last_outbox_id) VALUES (:u, :d, :loi)`,
		`UPDATE cmd_outbox_consumer_idx SET last_outbox_id = :loi WHERE user = :u AND device = :d`,
	}
	for _, q := range queries {
		if err := sqlitex.Execute(conn, q, &sqlitex.ExecOptions{
			Named: map[string]any{
				":u":   user,
				":d":   device,
				":loi": maxItem,
			},
		}); err != nil {
			slog.WarnContext(ctx, "failed to set max consumed id", slog.String("err", err.Error()))
			break
		}
	}

	return items, nil
}

func getUserID(_ context.Context, conn *sqlite.Conn, user string) (int, error) {
	var xid opt.Option[int]
	if err := sqlitex.Execute(conn, "SELECT id FROM users WHERE user = ?1", &sqlitex.ExecOptions{
		Args: []any{user},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			xid = opt.Some(stmt.ColumnInt(0))
			return nil
		},
	}); err != nil {
		return 0, errors.Wrap(err, "failed to lookup existing user id")
	}
	if existingID, ok := xid.MaybeUnwrap(); ok {
		return existingID, nil
	}

	var createdID opt.Option[int]
	if err := sqlitex.Execute(conn, "INSERT INTO users (user) VALUES (?1) RETURNING id", &sqlitex.ExecOptions{
		Args: []any{user},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			createdID = opt.Some(stmt.ColumnInt(0))
			return nil
		},
	}); err != nil {
		return 0, errors.Wrap(err, "failed to insert new user")
	}
	return createdID.Unwrap(), nil
}

func PubEndpoint(r *chi.Mux, cfg config, liveLoc *liveLocations, db *sqlitemigration.Pool) {
	r.
		With(
			middleware.AllowContentType("application/json"),
		).
		Post("/pub", ep.New(
			func(ctx context.Context, request PubRequest) (PubResponse, error) {
				if request.User != basicauth.VerifiedUsername(ctx).UnwrapOrZero() {
					return PubResponse{}, badRequest("input data and auth data mismatch")
				}
				if request.User == "" || request.Device == "" {
					return PubResponse{}, badRequest("user and device input is required")
				}

				otdata, err := decodeOTJSON(request.Body)
				if err != nil {
					return PubResponse{}, badRequest("failed to decode ot json: %s", err.Error())
				}

				bcast := func() {}
				switch otdata := otdata.(type) {
				case otLocation:
					if err := enrichOTLocationData(ctx, request.User, request.Device, otdata); err != nil {
						return PubResponse{}, errors.WithStack(err)
					}
					bcast = func() {
						liveLoc.broadcast(otdata)
					}
				}

				conn, err := db.Get(ctx)
				if err != nil {
					slog.Error("db error", slog.String("err", err.Error()))
					return PubResponse{}, srvError("failed connect to db")
				}
				defer db.Put(conn)

				userID, err := getUserID(ctx, conn, request.User)
				if err != nil {
					return PubResponse{}, errors.Wrap(err, "failed to get user id")
				}

				const insertSQL = `
					INSERT INTO location_reports (user_id, device, data)
					VALUES (?1, ?2, ?3)
				`
				err = sqlitex.Execute(conn, insertSQL, &sqlitex.ExecOptions{
					Args: []any{
						userID,
						request.Device,
						string(mustJSONEncode(otdata)),
					},
				})
				if err != nil {
					slog.Error("db error", slog.String("err", err.Error()))
					return PubResponse{}, srvError("failed to talk to db")
				}
				go bcast()
				if c := conn.Changes(); c != 1 {
					slog.WarnContext(ctx, "unexpected number of rows changed, expected 1", slog.Int("got", c))
				}

				outbox, err := checkOutbox(ctx, conn, request.User, request.Device)
				if err != nil {
					outbox = []map[string]any{} // to ensure the json rendered is "[]" not "null"
					slog.WarnContext(ctx, "failed to check outbox", slog.String("err", err.Error()))
				}

				return PubResponse{
					Messages: outbox,
				}, nil
			},
			ep.AutoDecode[PubRequest](),
			ep.EncodeJSONResponse,
		).ServeHTTP)
}
