package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	L *slog.Logger
	logDir  string
	file    *os.File
	mu      sync.Mutex
	curDate string
)

func init() {
	L = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func Init(dir string) error {
	logDir = filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("创建日志目录 %s 失败: %w", logDir, err)
	}

	rotate()

	fh := newFileHandler()
	eh := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})

	L = slog.New(newFanoutHandler([]slog.Handler{fh, eh}))

	go rotator()

	Info("日志系统初始化完成", "dir", logDir)
	return nil
}

func Debug(msg string, args ...any) { L.Debug(msg, args...) }
func Info(msg string, args ...any)  { L.Info(msg, args...) }
func Warn(msg string, args ...any)  { L.Warn(msg, args...) }
func Error(msg string, args ...any) { L.Error(msg, args...) }

func rotate() {
	mu.Lock()
	defer mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if file != nil && curDate == today {
		return
	}

	path := filepath.Join(logDir, today+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开日志文件 %s 失败: %v\n", path, err)
		return
	}
	if file != nil {
		file.Close()
	}
	file = f
	curDate = today

	cleanup()
}

func cleanup() {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	logFiles := []string{}
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".log") {
			logFiles = append(logFiles, filepath.Join(logDir, n))
		}
	}
	sort.Strings(logFiles)
	if len(logFiles) > 7 {
		for _, old := range logFiles[:len(logFiles)-7] {
			os.Remove(old)
		}
	}
}

func rotator() {
	for {
		time.Sleep(30 * time.Second)
		rotate()
	}
}

type fanoutHandler struct {
	handlers []slog.Handler
}

func newFanoutHandler(handlers []slog.Handler) *fanoutHandler {
	return &fanoutHandler{handlers: handlers}
}

func (h *fanoutHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, hh := range h.handlers {
		if hh.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	mu.Lock()
	defer mu.Unlock()
	for _, hh := range h.handlers {
		if hh.Enabled(ctx, r.Level) {
			if err := hh.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, hh := range h.handlers {
		handlers[i] = hh.WithAttrs(attrs)
	}
	return &fanoutHandler{handlers: handlers}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, hh := range h.handlers {
		handlers[i] = hh.WithGroup(name)
	}
	return &fanoutHandler{handlers: handlers}
}

type fileHandler struct {
	mu sync.Mutex
}

func newFileHandler() *fileHandler { return &fileHandler{} }

func (h *fileHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *fileHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	mu.Lock()
	f := file
	mu.Unlock()
	if f == nil {
		return nil
	}

	t := time.Now().Format("2006-01-02 15:04:05")
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(t)
	b.WriteString("] ")
	b.WriteString(r.Level.String())
	b.WriteString(" ")
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteString(" ")
		b.WriteString(a.Key)
		b.WriteString("=")
		b.WriteString(a.Value.String())
		return true
	})
	b.WriteString("\n")
	_, err := f.WriteString(b.String())
	return err
}

func (h *fileHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *fileHandler) WithGroup(_ string) slog.Handler      { return h }