package main

import (
	"bytes"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"

	"github.com/go-chi/chi/v5"
	"zombiezen.com/go/sqlite/sqlitemigration"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type liveLocations struct {
	l   sync.RWMutex
	in  chan otLocation
	out []chan<- otLocation
}

func newLiveLocations() *liveLocations {
	return &liveLocations{
		in: make(chan otLocation, 10),
	}
}

func (l *liveLocations) broadcast(d otLocation) {
	l.l.RLock()
	defer l.l.RUnlock()
	for _, oc := range l.out {
		oc <- d
	}
}

func (l *liveLocations) subscribe(c chan<- otLocation) func() {
	l.l.Lock()
	defer l.l.Unlock()
	l.out = append(l.out, c)
	idx := len(l.out) - 1
	return func() {
		l.l.Lock()
		defer l.l.Unlock()
		l.out = slices.Delete(l.out, idx, idx+1)
		close(c)
	}
}

type wsMessage struct {
	typ int
	p   []byte
}

func wsConsume(ws *websocket.Conn) <-chan wsMessage {
	c := make(chan wsMessage)
	go func() {
		defer close(c)
		for {
			t, p, err := ws.ReadMessage()
			if err != nil {
				return
			}
			c <- wsMessage{typ: t, p: p}
		}
	}()
	return c
}

func toLiveLocMessage(loc otLocation) map[string]any {
	copy := maps.Clone(loc)
	copy["_label"] = "OwnTracks"
	return copy
}

func WebsocketLastLocationEndpoint(r *chi.Mux, l *liveLocations, db *sqlitemigration.Pool) {
	r.Get("/ws/last", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.ErrorContext(r.Context(), "ws upgrader failed", slog.String("err", err.Error()))
			return
		}
		defer ws.Close()

		updates := make(chan otLocation, 5)
		unsubscribe := l.subscribe(updates)
		defer unsubscribe()

		inMessages := wsConsume(ws)

		ready := false
		for {
			select {
			case l := <-updates:
				_ = ws.WriteMessage(websocket.TextMessage, mustJSONEncode(toLiveLocMessage(l)))
			case in, ok := <-inMessages:
				if !ok {
					break
				}
				if !ready {
					if in.typ == websocket.TextMessage && bytes.EqualFold([]byte("LAST"), in.p) {
						ready = true
					} else {
						slog.WarnContext(r.Context(), "unexpected first message", slog.String("msg", string(in.p)))
					}
					continue
				}
			}
		}
	})
}
