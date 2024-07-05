package basicauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"code.nkcmr.net/opt"
	"golang.org/x/crypto/bcrypt"
)

type CredentialStore interface {
	Check(ctx context.Context, username, password string) (bool, error)
}

type InMemoryCredStore map[string]string

func (i InMemoryCredStore) Check(_ context.Context, username, password string) (bool, error) {
	hashpw, ok := i[username]
	if !ok {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(hashpw), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

type ctxKeyVerifiedUsername struct{}

func VerifiedUsername(ctx context.Context) opt.Option[string] {
	u, ok := ctx.Value(ctxKeyVerifiedUsername{}).(string)
	return opt.FromMaybe(u, ok)
}

func Middleware(realm string, cs CredentialStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok {
				basicAuthFailed(w, realm)
				return
			}

			ok, err := cs.Check(r.Context(), user, pass)
			if err != nil {
				slog.Error("basic_auth_cred_store_error", slog.String("err", err.Error()))
				http.Error(w, "authorization failed with an internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				basicAuthFailed(w, realm)
				return
			}
			next.ServeHTTP(
				w,
				r.WithContext(
					context.WithValue(r.Context(), ctxKeyVerifiedUsername{}, user),
				),
			)
		})
	}
}

func basicAuthFailed(w http.ResponseWriter, realm string) {
	w.Header().Add("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
	w.WriteHeader(http.StatusUnauthorized)
}
