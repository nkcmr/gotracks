package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"github.com/spf13/afero/zipfs"
)

const frontendVersion = "v2.15.3"

func downloadFrontend() (fs.FS, error) {
	client := cleanhttp.DefaultClient()
	uri := fmt.Sprintf(
		"https://github.com/owntracks/frontend/releases/download/%s/%s-dist.zip",
		frontendVersion,
		frontendVersion,
	)
	slog.Info("downloading frontend", slog.String("uri", uri))
	req, err := http.NewRequest(
		"GET",
		uri,
		http.NoBody,
	)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		go io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("non-ok status returned from github: %d", resp.StatusCode)
	}

	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	zipr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	zipfsDist, err := fs.Sub(afero.NewIOFS(zipfs.New(zipr)), "dist")
	if err != nil {
		return nil, err
	}
	return zipfsDist, nil
}

func serveFrontend(r *chi.Mux) error {
	feFS, err := downloadFrontend()
	if err != nil {
		return errors.Wrap(err, "failed to download frontend")
	}
	r.Get("/config/config.js", func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Content-Type", "application/javascript")
		io.WriteString(w, `window.owntracks = window.owntracks || {};
window.owntracks.config = {};
`)
	})
	r.Mount("/", http.FileServerFS(feFS))
	return nil
}
