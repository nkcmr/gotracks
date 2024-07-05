package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// mirrorPub is just a crappy hack i have to mirror POST /pub calls to a
// owntracks/recorder server i have so that i can use real data to reverse
// engineer some things a bit easier.
func mirrorPub(r *chi.Mux, cfg config) {
	r.Use(
		middleware.Maybe(
			func(h http.Handler) http.Handler {
				if cfg.Server.MirrorTo == "" {
					return h
				}
				mirrorURL, err := url.ParseRequestURI(cfg.Server.MirrorTo)
				if err != nil {
					panic(fmt.Sprintf("invalid SERVER_MIRROR_TO config: %s", err.Error()))
				}
				rp := httputil.NewSingleHostReverseProxy(mirrorURL)
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, fmt.Sprintf("failed to read request body: %s", err.Error()), http.StatusInternalServerError)
						return
					}
					_ = r.Body.Close()
					r.Body = io.NopCloser(bytes.NewReader(body))

					r2 := r.Clone(context.WithoutCancel(r.Context()))
					r2.Body = io.NopCloser(bytes.NewReader(body))

					go func() {
						rp.ServeHTTP(noopResponseWriter{}, r2)
						slog.InfoContext(r.Context(), "mirrored request")
					}()
					h.ServeHTTP(w, r)
				})
			},
			func(r *http.Request) bool {
				return r.Method == "POST" && r.URL.Path == "/pub"
			},
		),
	)
}

type noopResponseWriter struct{}

var _ http.ResponseWriter = noopResponseWriter{}

func (noopResponseWriter) Header() http.Header         { return http.Header{} }
func (noopResponseWriter) Write(d []byte) (int, error) { return len(d), nil }
func (noopResponseWriter) WriteHeader(int)             {}
