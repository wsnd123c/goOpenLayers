package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var sqlFieldRegexp = regexp.MustCompile(` sql="((?:\\.|[^"\\])*)"`)

type consoleHandler struct {
	out    io.Writer
	opts   slog.HandlerOptions
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

// NewConsoleHandler returns a compact handler intended for humans reading local logs.
func NewConsoleHandler(out io.Writer, opts *slog.HandlerOptions) slog.Handler {
	handlerOptions := slog.HandlerOptions{}
	if opts != nil {
		handlerOptions = *opts
	}

	return &consoleHandler{
		out:  out,
		opts: handlerOptions,
		mu:   &sync.Mutex{},
	}
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	when := r.Time
	if when.IsZero() {
		when = time.Now()
	}

	b.WriteString(when.Format("2006-01-02 15:04:05"))
	b.WriteByte(' ')
	b.WriteString(formatLevel(r.Level))

	if h.opts.AddSource && r.PC != 0 {
		if fs := runtime.CallersFrames([]uintptr{r.PC}); fs != nil {
			frame, _ := fs.Next()
			b.WriteByte(' ')
			b.WriteString(shortFile(frame.File))
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(frame.Line))
		}
	}

	if r.Message != "" {
		b.WriteByte(' ')
		b.WriteString(formatMessage(r.Message))
	}

	attrs := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	for _, attr := range attrs {
		attr = h.replaceAttr(attr)
		if attr.Equal(slog.Attr{}) {
			continue
		}

		formatted := h.formatAttr(attr)
		if formatted == "" {
			continue
		}
		if attr.Key == "stack" {
			b.WriteByte('\n')
			b.WriteString(formatted)
			continue
		}
		b.WriteByte(' ')
		b.WriteString(formatted)
	}

	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.groups = append(append([]string{}, h.groups...), name)
	return &next
}

func (h *consoleHandler) replaceAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if h.opts.ReplaceAttr == nil {
		return attr
	}
	return h.opts.ReplaceAttr(h.groups, attr)
}

func (h *consoleHandler) formatAttr(attr slog.Attr) string {
	key := attr.Key
	if key == "" {
		return ""
	}
	if len(h.groups) > 0 {
		key = strings.Join(append(append([]string{}, h.groups...), key), ".")
	}

	if attr.Key == "stack" {
		return "stack:\n" + indent(strings.TrimRight(attr.Value.String(), "\n"), "  ")
	}

	return key + "=" + formatValue(attr.Value)
}

func formatLevel(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return "DEBUG"
	case level < slog.LevelWarn:
		return "INFO "
	case level < slog.LevelError:
		return "WARN "
	default:
		return "ERROR"
	}
}

func formatMessage(msg string) string {
	matches := sqlFieldRegexp.FindStringSubmatchIndex(msg)
	if matches == nil {
		return msg
	}

	quotedSQL := msg[matches[2]:matches[3]]
	sqlText, err := strconv.Unquote(`"` + quotedSQL + `"`)
	if err != nil {
		return msg
	}

	prefix := strings.TrimSpace(msg[:matches[0]])
	suffix := strings.TrimSpace(msg[matches[1]:])

	var b strings.Builder
	b.WriteString(prefix)
	if suffix != "" {
		b.WriteByte(' ')
		b.WriteString(suffix)
	}
	b.WriteString("\n  sql: ")
	b.WriteString(indentSQL(sqlText))
	return b.String()
}

func formatValue(value slog.Value) string {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return quoteIfNeeded(value.String())
	case slog.KindTime:
		return value.Time().Format("2006-01-02 15:04:05")
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindGroup:
		attrs := value.Group()
		parts := make([]string, 0, len(attrs))
		for _, attr := range attrs {
			parts = append(parts, attr.Key+"="+formatValue(attr.Value))
		}
		return "{" + strings.Join(parts, " ") + "}"
	default:
		return quoteIfNeeded(fmt.Sprint(value.Any()))
	}
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\n\r\"=") {
		return strconv.Quote(value)
	}
	return value
}

func shortFile(path string) string {
	path = strings.ReplaceAll(path, `\`, `/`)
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		return path
	}
	return path[idx+1:]
}

func indentSQL(sqlText string) string {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		" WITH ", "\n       WITH ",
		" SELECT ", "\n       SELECT ",
		" FROM ", "\n       FROM ",
		" WHERE ", "\n       WHERE ",
		" ORDER BY ", "\n       ORDER BY ",
		" LIMIT ", "\n       LIMIT ",
	)
	return replacer.Replace(sqlText)
}

func indent(text, prefix string) string {
	if text == "" {
		return prefix
	}
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
}
