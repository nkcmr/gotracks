package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"code.nkcmr.net/gotracks/internal/ep"
	"code.nkcmr.net/opt"

	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

type LastLocationRequest struct {
	User   string `query:"user"`
	Device string `query:"device"`
}

type LastLocationResponse struct {
	Locations []LastLocationResponse_Location
}

func (l LastLocationResponse) APIResponse() any {
	return l.Locations
}

type LastLocationResponse_Location map[string]any

func optFromZero[V comparable](v V) opt.Option[V] {
	var zv V
	if v == zv {
		return opt.None[V]()
	}
	return opt.Some(v)
}

func mapSlice[S ~[]I, I, O any](s S, mf func(I) O) []O {
	out := make([]O, 0, len(s))
	for i := range s {
		out = append(out, mf(s[i]))
	}
	return out
}

func LastLocationEndpoint(r *chi.Mux, db *sqlitemigration.Pool) {
	r.Get("/api/0/last", ep.New(
		func(ctx context.Context, request LastLocationRequest) (LastLocationResponse, error) {
			conn, err := db.Get(ctx)
			if err != nil {
				return LastLocationResponse{}, errors.Wrap(err, "failed to get db conn")
			}
			defer db.Put(conn)

			locs, err := lastLocation(
				ctx, conn,
				optFromZero(request.User), optFromZero(request.Device),
			)
			if err != nil {
				return LastLocationResponse{}, errors.WithStack(err)
			}
			return LastLocationResponse{
				Locations: mapSlice(locs, func(in otLocation) LastLocationResponse_Location {
					return LastLocationResponse_Location(in)
				}),
			}, nil
		},
		ep.AutoDecode[LastLocationRequest](),
		ep.EncodeJSONResponse,
	).ServeHTTP)
}

func lastLocation(
	_ context.Context, conn *sqlite.Conn,
	user, device opt.Option[string],
) ([]otLocation, error) {
	const query = `
		WITH last_location_report AS (
			SELECT MAX(id) AS id
			FROM location_reports
			WHERE %s
			GROUP BY user_id, device
		)
		SELECT lr.id, u.user, lr.device, lr.data
		FROM location_reports AS lr
		INNER JOIN users AS u ON lr.user_id = u.id
		WHERE lr.id IN (SELECT id FROM last_location_report)
	`
	conds := []string{}
	args := []any{}

	if u, ok := user.MaybeUnwrap(); ok {
		conds = append(conds, "user_id = (SELECT id FROM users WHERE user = ?1)")
		args = append(args, u)
	}
	if d, ok := device.MaybeUnwrap(); ok {
		conds = append(conds, "device = ?2")
		args = append(args, d)
	}
	if len(conds) == 0 {
		conds = []string{"1 = 1"}
	}
	type row struct {
		id       int
		username string
		device   string
		data     json.RawMessage
	}
	var rows []row
	if err := sqlitex.Execute(
		conn,
		fmt.Sprintf(query, strings.Join(conds, " AND ")),
		&sqlitex.ExecOptions{
			Args: args,
			ResultFunc: func(stmt *sqlite.Stmt) error {
				var r row
				r.id = stmt.ColumnInt(0)
				r.username = stmt.ColumnText(1)
				r.device = stmt.ColumnText(2)
				r.data = json.RawMessage(stmt.ColumnText(3))
				rows = append(rows, r)
				return nil
			},
		},
	); err != nil {
		return nil, errors.Wrap(err, "query failed")
	}

	locs := []otLocation{}
	for _, r := range rows {
		var l otLocation
		if err := json.Unmarshal(r.data, &l); err != nil {
			return nil, errors.Wrap(err, "corrupt location data")
		}
		locs = append(locs, l)
	}

	return locs, nil
}
