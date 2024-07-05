package main

import (
	"encoding/json"
	"fmt"
	"time"

	"code.nkcmr.net/opt"
	"github.com/pkg/errors"
	"github.com/valyala/fastjson"
)

type otJSON interface {
	isOTJSON()
}

func decodeOTJSON(d []byte) (otJSON, error) {
	switch typ := fastjson.GetString(d, "_type"); typ {
	case "location":
		return decodeOTLocationJSON(d)
	// case "status":
	default:
		return nil, fmt.Errorf("unknown ot json type: %q", typ)
	}
}

type otLocation map[string]any

func readFloat64[M ~map[string]any](m M, key string) opt.Option[float64] {
	v, ok := m[key].(float64)
	if !ok {
		return opt.None[float64]()
	}
	return opt.Some(v)
}

func readInt[M ~map[string]any](m M, key string) opt.Option[int] {
	return opt.Map(readFloat64(m, key), func(v float64) opt.Option[int] {
		return opt.Some(int(v))
	})
}

func readString[M ~map[string]any](m M, key string) opt.Option[string] {
	v, ok := m[key]
	if ok {
		switch v := v.(type) {
		case string:
			return opt.Some(v)
		case float64, bool:
			return opt.Some(fmt.Sprintf("%v", v))
		}
	}
	return opt.None[string]()
}

// Accuracy of the reported location in meters without unit (iOS,Android/integer/meters/optional)
func (o otLocation) Accuracy() opt.Option[int] {
	return readInt(o, "acc")
}

// Trigger for the location report (iOS,Android/string/optional)
func (o otLocation) Trigger() opt.Option[string] {
	return readString(o, "t")
}

func (o otLocation) LatLng() opt.Option[Point] {
	return opt.Join(
		readFloat64(o, "lat"), readFloat64(o, "lon"),
		func(lat, lon float64) Point {
			return Point{lat, lon}
		},
	)
}

func (o otLocation) Topic() opt.Option[string] {
	return readString(o, "topic")
}

func (o otLocation) Timestamp() opt.Option[time.Time] {
	return opt.Map(readInt(o, "tst"), func(tst int) opt.Option[time.Time] {
		return opt.Some(time.Unix(int64(tst), 0))
	})
}

// const (
// 	// ping issued randomly by background task (iOS,Android)
// 	otTriggerPing = "p"
// 	// circular region enter/leave event (iOS,Android)
// 	otTriggerCircle = "c"
// 	// beacon region enter/leave event (iOS)
// 	otTriggerBeacon = "b"
// 	// response to a reportLocation cmd message (iOS,Android)
// 	otTriggerReport = "r"
// 	// manual publish requested by the user (iOS,Android)
// 	otTriggerManual = "u"
// 	// timer based publish in move move (iOS)
// 	otTriggerTimer = "t"
// 	// updated by `Settings/Privacy/Locations Services/System Services/Frequent Locations` monitoring (iOS)
// 	otTriggerSigLoc = "v"
// )

func (otLocation) isOTJSON() {}

func decodeOTLocationJSON(d []byte) (otLocation, error) {
	var out otLocation

	if err := json.Unmarshal(d, &out); err != nil {
		return nil, errors.Wrap(err, "failed to json decode")
	}

	return out, nil
}

func mustJSONEncode(v any) []byte {
	d, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSONEncode: failed: %s", err.Error()))
	}
	return d
}
