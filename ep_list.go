package main

import (
	"context"
	"fmt"

	"code.nkcmr.net/gotracks/internal/ep"

	"github.com/go-chi/chi/v5"
	"github.com/pkg/errors"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitemigration"
	"zombiezen.com/go/sqlite/sqlitex"
)

type ListRequest struct {
	User   string `query:"user"`
	Device string `query:"device"`
}

type ListResponse struct {
	Results []string `json:"results"`
}

func ListEndpoint(r *chi.Mux, db *sqlitemigration.Pool) {
	r.Get("/api/0/list", ep.New(
		func(ctx context.Context, request ListRequest) (ListResponse, error) {
			conn, err := db.Get(ctx)
			if err != nil {
				return ListResponse{}, errors.Wrap(err, "failed to get db conn")
			}
			defer db.Put(conn)

			result := []string{}
			if request.User == "" && request.Device == "" {
				if err := sqlitex.Execute(conn, "SELECT user FROM users", &sqlitex.ExecOptions{
					ResultFunc: func(stmt *sqlite.Stmt) error {
						result = append(result, stmt.ColumnText(0))
						return nil
					},
				}); err != nil {
					return ListResponse{}, errors.Wrap(err, "query failed")
				}
			} else if request.Device == "" {
				const query = `
					SELECT DISTINCT device
					FROM location_reports AS l
					INNER JOIN users AS u ON l.user_id = u.id
					WHERE u.user = ?1
				`
				if err := sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
					Args: []any{
						request.User,
					},
					ResultFunc: func(stmt *sqlite.Stmt) error {
						result = append(result, stmt.ColumnText(0))
						return nil
					},
				}); err != nil {
					return ListResponse{}, errors.Wrap(err, "query failed")
				}
			} else {
				return ListResponse{}, fmt.Errorf("unsupported")
			}

			return ListResponse{
				Results: result,
			}, nil
		},
		ep.AutoDecode[ListRequest](),
		ep.EncodeJSONResponse,
	).ServeHTTP)
}
