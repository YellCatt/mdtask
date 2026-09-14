// Package logger 是基于 slog 的日志封装：日志同时写入「按天切分+保留 7 天」的
// 本地文件与 stderr，并对外提供 Debug/Info/Warn/Error 快捷函数。
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
	L       *slog.Logger
	logDir  string
	handler *fileHandler
)

func init() {
	L = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// Init 创建 logs 目录、初始化按天轮转的文件 handler，并启动后台轮转 goroutine。
func Init() error {
	logDir = "./logs"
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = abs
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("创建日志目录 %s 失败: %w", logDir, err)
	}

	handler = newFileHandler()
	if err := handler.rotate(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 初始日志轮转失败: %v\n", err)
	}

	fh := slog.Handler(handler)
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

// rotator 每 30 秒尝试一次轮转，跨天后会自动切到新的日期日志文件。
func rotator() {
	for {
		time.Sleep(30 * time.Second)
		if handler != nil {
			_ = handler.rotate()
		}
	}
}

// fanoutHandler 把同一条日志广播给多个底层 handler（文件 + stderr）。
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

// fileHandler 把日志按「日期.log」追加写入文件，并在跨天或启动时自动切换文件。
type fileHandler struct {
	mu      sync.Mutex
	file    *os.File
	curDate string
}

func newFileHandler() *fileHandler { return &fileHandler{} }

func (h *fileHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *fileHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.checkRotateLocked(); err != nil {
		fmt.Fprintf(os.Stderr, "日志轮转失败: %v\n", err)
	}

	if h.file == nil {
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
	_, err := h.file.WriteString(b.String())
	return err
}

func (h *fileHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *fileHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *fileHandler) rotate() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.checkRotateLocked()
}

// checkRotateLocked 若日期变化或文件未打开则重新打开当日日志文件，并清理旧日志。
// 调用方需持有 h.mu。
func (h *fileHandler) checkRotateLocked() error {
	today := time.Now().Format("2006-01-02")
	if h.file != nil && h.curDate == today {
		return nil
	}

	path := filepath.Join(logDir, today+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件 %s 失败: %w", path, err)
	}
	if h.file != nil {
		h.file.Close()
	}
	h.file = f
	h.curDate = today

	cleanup()
	return nil
}

// cleanup 只保留最近 7 个 .log 文件，删除更早的，避免日志无限增长。
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