package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"code.nkcmr.net/gotracks/internal/ep"
	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

type LocationsRequest struct {
	From   string `query:"from"`
	To     string `query:"to"`
	User   string `query:"user"`
	Device string `query:"device"`
	Format string `query:"format"`
}

type LocationsResponse struct {
	Count   int                      `json:"count"`
	Data    []LocationsResponse_Data `json:"data"`
	Status  int                      `json:"status"`
	Version string                   `json:"version"`
}

type LocationsResponse_Data map[string]any

func LocationsEndpoint(r *chi.Mux, db *sqlitemigration.Pool) {
	r.Get("/api/0/locations", ep.New(
		func(ctx context.Context, request LocationsRequest) (LocationsResponse, error) {
			switch request.Format {
			case "", "json":
			default:
				return LocationsResponse{}, badRequest("unsupported format: %q", request.Format)
			}

			const query = `
				SELECT lr.data
				FROM location_reports AS lr
				INNER JOIN users AS u ON lr.user_id = u.id
				WHERE %s
				ORDER BY lr.id ASC
			`
			conds := []string{}
			args := []any{}

			const tsFormat = "2006-01-02T15:04:05"
			if request.From != "" {
				from, err := time.ParseInLocation(tsFormat, request.From, time.UTC)
				if err != nil {
					return LocationsResponse{}, badRequest(`failed to parse "from": %s`, err.Error())
				}
				args = append(args, from.Unix())
				conds = append(conds, fmt.Sprintf("json_extract(data, '$.tst') >= ?%d", len(args)))
			}
			if request.To != "" {
				to, err := time.ParseInLocation(tsFormat, request.To, time.UTC)
				if err != nil {
					return LocationsResponse{}, badRequest(`failed to parse "to": %s`, err.Error())
				}
				args = append(args, to.Unix())
				conds = append(conds, fmt.Sprintf("json_extract(data, '$.tst') <= ?%d", len(args)))
			}
			if request.User != "" {
				args = append(args, request.User)
				conds = append(conds, fmt.Sprintf("u.user = ?%d", len(args)))
			}
			if request.Device != "" {
				args = append(args, request.Device)
				conds = append(conds, fmt.Sprintf("lr.device = ?%d", len(args)))
			}

			if len(conds) == 0 {
				conds = []string{"1 = 1"}
			}

			conn, err := db.Get(ctx)
			if err != nil {
				return LocationsResponse{}, errors.Wrap(err, "failed to connect to db")
			}
			defer db.Put(conn)

			var locs []LocationsResponse_Data
			if err := sqlitex.Execute(conn, fmt.Sprintf(query, strings.Join(conds, " AND ")), &sqlitex.ExecOptions{
				Args: args,
				ResultFunc: func(stmt *sqlite.Stmt) error {
					var row LocationsResponse_Data
					if err := json.Unmarshal([]byte(stmt.ColumnText(0)), &row); err != nil {
						return errors.Wrap(err, "corrupt db data")
					}
					locs = append(locs, row)
					return nil
				},
			}); err != nil {
				return LocationsResponse{}, errors.Wrap(err, "query failed")
			}

			return LocationsResponse{
				Count:   len(locs),
				Data:    locs,
				Status:  200,
				Version: "0.0.1",
			}, nil
		},
		ep.AutoDecode[LocationsRequest](),
		ep.EncodeJSONResponse,
	).ServeHTTP)
}
