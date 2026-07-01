package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// idParam parses the :id path parameter. On failure it writes a 400
// response and returns ok=false.
func idParam(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// ingestAuth gates an ingest endpoint behind an X-Api-Key header when key
// is non-empty. A no-op when the key is unset (in-cluster posture), a hard
// 401 on mismatch when set. label names the endpoint in rejection logs.
func ingestAuth(key, label string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if key == "" {
			c.Next()
			return
		}
		if c.GetHeader("X-Api-Key") != key {
			slog.Warn(label+": rejected",
				slog.String("ip", c.ClientIP()),
				slog.Bool("header_present", c.GetHeader("X-Api-Key") != ""))
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				gin.H{"error": "invalid or missing X-Api-Key"})
			return
		}
		c.Next()
	}
}

// sseHeaders prepares the response for server-sent events and emits the
// initial ": connected" comment. Returns false when the write failed.
func sseHeaders(c *gin.Context) bool {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(c.Writer, ": connected\n\n"); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}

// sseEvent writes one SSE event. Returns false when the client is gone;
// events that fail to marshal are skipped with the stream kept alive.
func sseEvent(c *gin.Context, event string, v any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return true
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}

// sseLoop pumps events from ch to the client with a 25 s keepalive (which
// keeps idle connections open through proxies) until the client disconnects
// or ch closes. eventName extracts the SSE event name from an event.
func sseLoop[T any](c *gin.Context, ch <-chan T, eventName func(T) string) {
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(c.Writer, ": keepalive\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if !sseEvent(c, eventName(ev), ev) {
				return
			}
		}
	}
}
