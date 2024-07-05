package ep

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"reflect"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-kit/kit/transport"
	"github.com/go-kit/kit/transport/http"
	"github.com/go-kit/log"
	"github.com/pkg/errors"
)

type Endpoint[Request, Response any] func(ctx context.Context, request Request) (response Response, err error)
type DecodeRequestFunc[Request any] func(context.Context, *gohttp.Request) (request Request, err error)
type EncodeResponseFunc[Response any] func(context.Context, gohttp.ResponseWriter, Response) error
type Option = http.ServerOption
type Server = http.Server

func EncodeJSONResponse[Response any](ctx context.Context, w gohttp.ResponseWriter, response Response) error {
	resp := any(response)
	if a, ok := resp.(interface{ APIResponse() any }); ok {
		resp = a.APIResponse()
	}
	return http.EncodeJSONResponse(ctx, w, resp)
}

var _ log.Logger = slogger{}

type slogger struct{}

func (slogger) Log(keyvals ...interface{}) error {
	stuff := []any{}
	for i := range len(keyvals) {
		if i > 0 && i%2 == 0 {
			stuff = append(stuff, slog.Any(
				fmt.Sprintf("%v", keyvals[i-1]),
				keyvals[i],
			))
		}
	}
	slog.Info("slogger", stuff...)
	return nil
}

type errorHandler struct {
}

func (errorHandler) Handle(ctx context.Context, err error) {
	slog.Error("server_error", slog.String("error", err.Error()))
}

var _ transport.ErrorHandler = errorHandler{}

func New[Request, Response any](
	ep Endpoint[Request, Response],
	dec DecodeRequestFunc[Request],
	enc EncodeResponseFunc[Response],
	options ...Option,
) *Server {
	return http.NewServer(
		func(ctx context.Context, request interface{}) (response interface{}, err error) {
			return ep(ctx, request.(Request))
		},
		func(ctx context.Context, r *gohttp.Request) (request interface{}, err error) {
			return dec(ctx, r)
		},
		func(ctx context.Context, w gohttp.ResponseWriter, i interface{}) error {
			return enc(ctx, w, i.(Response))
		},
		append(options, http.ServerErrorHandler(errorHandler{}))...,
	)
}

var signedIntBitSizeMap = map[reflect.Kind]int{
	reflect.Int:   64,
	reflect.Int8:  8,
	reflect.Int16: 16,
	reflect.Int32: 32,
	reflect.Int64: 64,
}
var unsignedIntBitSizeMap = map[reflect.Kind]int{
	reflect.Uint:   64,
	reflect.Uint8:  8,
	reflect.Uint16: 16,
	reflect.Uint32: 32,
	reflect.Uint64: 64,
}

func reqValueExtracter[Request any](
	getvalue func(r *gohttp.Request, key string) []string,
	keyFields map[string]int,
) func(ctx context.Context, r *gohttp.Request, request *Request) error {

	return func(ctx context.Context, r *gohttp.Request, request *Request) error {
		rv := reflect.ValueOf(request).Elem()
		for key, fieldIndex := range keyFields {
			sf := rv.Type().Field(fieldIndex)
			values := getvalue(r, key)
			if len(values) == 0 {
				continue
			}
			switch sf.Type.Kind() {
			case reflect.String:
				rv.Field(fieldIndex).Set(reflect.ValueOf(values[0]))
			case reflect.Bool:
				b, err := strconv.ParseBool(values[0])
				if err != nil {
					return errors.Wrapf(err, `failed to parse "%s" as boolean`, key)
				}
				rv.Field(fieldIndex).Set(reflect.ValueOf(b).Convert(sf.Type))
			default:
				if bitSize, ok := signedIntBitSizeMap[sf.Type.Kind()]; ok {
					n, err := strconv.ParseInt(values[0], 10, bitSize)
					if err != nil {
						return errors.Wrapf(err, "failed to parse %d bit int", bitSize)
					}
					rv.Field(fieldIndex).Set(reflect.ValueOf(n).Convert(sf.Type))
				} else if bitSize, ok := unsignedIntBitSizeMap[sf.Type.Kind()]; ok {
					n, err := strconv.ParseUint(values[0], 10, bitSize)
					if err != nil {
						return errors.Wrapf(err, "failed to parse %d bit uint", bitSize)
					}
					rv.Field(fieldIndex).Set(reflect.ValueOf(n).Convert(sf.Type))
				} else {
					return fmt.Errorf("unsupported reflection kind %s", sf.Type.Kind())
				}
			}
		}
		return nil
	}
}

func AutoDecode[Request any]() func(ctx context.Context, r *gohttp.Request) (Request, error) {
	steps := []func(ctx context.Context, r *gohttp.Request, request *Request) error{}
	rt := reflect.TypeFor[Request]()
	queryVars := map[string]int{}
	routeVars := map[string]int{}
	headerVars := map[string]int{}
	doJsonDecode := false
	doPassDirectBody := int(-1)
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if queryKey := sf.Tag.Get("query"); queryKey != "" {
			queryVars[queryKey] = i
		}
		if routeKey := sf.Tag.Get("route"); routeKey != "" {
			routeVars[routeKey] = i
		}
		if headerKey := sf.Tag.Get("header"); headerKey != "" {
			headerVars[headerKey] = i
		}
		if sf.Tag.Get("json") != "" {
			doJsonDecode = true
		}
		if sf.Name == "Body" && sf.Type == reflect.TypeFor[[]byte]() {
			if doPassDirectBody == -1 {
				doPassDirectBody = i
			}
		}
	}

	if doJsonDecode && (doPassDirectBody >= 0) {
		steps = append(steps, func(ctx context.Context, r *gohttp.Request, request *Request) error {
			defer r.Body.Close()
			reqbytes, err := io.ReadAll(r.Body)
			if err != nil {
				return errors.Wrap(err, "failed to read request body")
			}

			// json decode
			if err := json.Unmarshal(reqbytes, request); err != nil {
				return errors.Wrap(err, "failed to json decode request body")
			}

			// assign request bytes to struct
			rv := reflect.ValueOf(request).Elem()
			rv.Field(doPassDirectBody).Set(reflect.ValueOf(reqbytes))
			return nil
		})
	} else if doJsonDecode {
		steps = append(steps, func(ctx context.Context, r *gohttp.Request, request *Request) error {
			defer r.Body.Close()
			jbytes, err := io.ReadAll(r.Body)
			if err != nil {
				return errors.Wrap(err, "failed to read request body")
			}
			if err := json.Unmarshal(jbytes, request); err != nil {
				return errors.Wrap(err, "failed to json decode request body")
			}
			return nil
		})
	} else if doPassDirectBody >= 0 {
		steps = append(steps, func(ctx context.Context, r *gohttp.Request, request *Request) error {
			defer r.Body.Close()
			reqbytes, err := io.ReadAll(r.Body)
			if err != nil {
				return errors.Wrap(err, "failed to read request body")
			}
			rv := reflect.ValueOf(request).Elem()
			rv.Field(doPassDirectBody).Set(reflect.ValueOf(reqbytes))
			return nil
		})
	}

	if len(queryVars) > 0 {
		steps = append(steps, reqValueExtracter[Request](
			func(r *gohttp.Request, key string) []string {
				return r.URL.Query()[key]
			},
			queryVars,
		))
	}
	if len(routeVars) > 0 {
		steps = append(steps, reqValueExtracter[Request](
			func(r *gohttp.Request, key string) []string {
				v := chi.URLParam(r, key)
				if v == "" {
					return nil
				}
				return []string{v}
			},
			routeVars,
		))
	}
	if len(headerVars) > 0 {
		steps = append(steps, reqValueExtracter[Request](
			func(r *gohttp.Request, key string) []string {
				return r.Header.Values(key)
			},
			headerVars,
		))
	}
	return func(ctx context.Context, r *gohttp.Request) (Request, error) {
		var req Request
		for _, s := range steps {
			if err := s(ctx, r, &req); err != nil {
				var zv Request
				return zv, errors.WithStack(err)
			}
		}
		return req, nil
	}
}
