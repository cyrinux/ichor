package ichorgo

import (
	"context"
	"log/slog"
	"slices"
)

// The core's log/slog output goes to the diagnostics ring (diag.go), through diagHandler.

func diagLevelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// diag records an entry from the core: attrs are slog key/value pairs (see the rule above).
func diag(level slog.Level, component, msg string, err error, attrs ...any) {
	r := slog.NewRecord(diagClock(), level, msg, 0)
	r.Add(attrs...)

	e := diagRecord(component, r, nil, "")
	if err != nil {
		e.Err = diagScrub(err.Error(), diagMaxErr)
	}

	diagLog.add(e)
}

// diagRecord turns an slog record, with the handler's own attributes, into an entry. An
// attribute "component" picks the component; one holding an error becomes the entry's err.
func diagRecord(component string, r slog.Record, pre []slog.Attr, group string) diagEntry {
	e := diagEntry{
		At: r.Time.UnixMilli(), Level: diagLevelName(r.Level), Component: component,
		Msg: diagScrub(r.Message, diagMaxMsg),
	}
	if r.Time.IsZero() {
		e.At = diagClock().UnixMilli()
	}

	attrs := map[string]any{}

	for _, a := range pre {
		diagAttr(&e, attrs, "", a)
	}

	r.Attrs(func(a slog.Attr) bool {
		diagAttr(&e, attrs, group, a)

		return true
	})

	if len(attrs) > 0 {
		e.Attrs = attrs
	}

	return e
}

// diagAttr adds one attribute to e, flattening groups as "group.key".
func diagAttr(e *diagEntry, attrs map[string]any, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}

	switch {
	case v.Kind() == slog.KindGroup:
		for _, g := range v.Group() {
			diagAttr(e, attrs, key, g)
		}

		return
	case a.Key == "component" && prefix == "":
		if c := v.String(); slices.Contains(diagComponents, c) {
			e.Component = c
		}

		return
	}

	if err, ok := v.Any().(error); ok && v.Kind() == slog.KindAny {
		e.Err = diagScrub(err.Error(), diagMaxErr)

		return
	}

	if len(attrs) >= diagMaxAttrs || key == "" {
		return
	}

	attrs[key] = diagValue(v)
}

// diagValue keeps numbers and bools as they are, durations and times as ms, and redacts
// anything else as text.
func diagValue(v slog.Value) any {
	switch v.Kind() {
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().Milliseconds()
	case slog.KindTime:
		return v.Time().UnixMilli()
	default:
		return diagTruncate(diagRedact(v.String()), diagMaxValue)
	}
}

// diagHandler is the slog.Handler of the whole core (see init): it appends to the ring.
// Below LevelInfo nothing is kept, so a chatty dependency cannot flood the ring.
type diagHandler struct {
	attrs []slog.Attr
	group string
}

func init() {
	slog.SetDefault(slog.New(diagHandler{}))
}

func (diagHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }

func (h diagHandler) Handle(_ context.Context, r slog.Record) error {
	diagLog.add(diagRecord("api", r, h.attrs, h.group))

	return nil
}

func (h diagHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := diagHandler{attrs: slices.Clone(h.attrs), group: h.group}
	for _, a := range attrs {
		if h.group != "" {
			a = slog.Group(h.group, a)
		}

		out.attrs = append(out.attrs, a)
	}

	return out
}

func (h diagHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	group := name
	if h.group != "" {
		group = h.group + "." + name
	}

	return diagHandler{attrs: h.attrs, group: group}
}
