package logger

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// GinMiddleware returns a Gin handler that logs every request.
//
//   - DEBUG level: all requests (method, path, query, status, latency, client IP, bytes)
//   - INFO  level: 4xx/5xx responses on any route; all /api/ responses
//   - WARN  level: 4xx responses
//   - ERROR level: 5xx responses
//
// Use this instead of gin.Default()'s built-in logger.
func GinMiddleware() gin.HandlerFunc {
	debug := IsDebug()
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		bytes := c.Writer.Size()
		if bytes < 0 {
			bytes = 0
		}
		clientIP := c.ClientIP()
		method := c.Request.Method
		errMsg := c.Errors.ByType(gin.ErrorTypePrivate).String()
		isAPI := len(path) >= 5 && path[:5] == "/api/"

		attrs := []any{
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("ip", clientIP),
			slog.Int("bytes", bytes),
		}
		if query != "" {
			attrs = append(attrs, slog.String("query", query))
		}
		if errMsg != "" {
			attrs = append(attrs, slog.String("error", errMsg))
		}
		if isAPI {
			if ua := c.Request.Header.Get("User-Agent"); ua != "" {
				attrs = append(attrs, slog.String("ua", ua))
			}
		}

		switch {
		case status >= 500:
			slog.Error("http", attrs...)
		case status >= 400:
			slog.Warn("http", attrs...)
		case isAPI || debug:
			// Always log successful API calls at Info so the server log
			// shows every request any API client makes.
			slog.Info("http", attrs...)
		}
	}
}
