package web

import (
	"html"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

func addWeightHandler(c *gin.Context) {
	var w models.BodyWeight

	w.Date = c.PostForm("date")
	var ok bool
	if w.Weight, ok = formDecimal(c.PostForm("weight")); !ok {
		weightRejected(c, http.StatusBadRequest,
			"“"+c.PostForm("weight")+"” is not a number.")
		return
	}

	// Same plausibility guard the API scale path enforces. formDecimal maps a
	// blank field to zero, which is below the floor, so this also rejects an
	// accidental empty submit that would otherwise persist a 0-lb weigh-in and
	// poison every body-weight chart.
	if conf.WeightOutOfRange(w.Weight) {
		slog.Warn("addWeightHandler: rejected out-of-range weight",
			slog.String("date", w.Date), slog.String("weight", w.Weight.String()))
		lo, hi := conf.WeightBounds()
		msg := "The weight field was empty."
		if !w.Weight.IsZero() {
			msg = w.Weight.String() + " lb is outside the accepted range."
		}
		weightRejected(c, http.StatusUnprocessableEntity,
			msg+" Enter a weight between "+lo.String()+" and "+hi.String()+" lb.")
		return
	}

	if err := dataStore.InsertW(c.Request.Context(), w); err != nil {
		slog.Error("addWeightHandler: InsertW failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	ref := c.Request.Referer()
	if ref == "" {
		ref = "/stats/"
	}
	c.Redirect(http.StatusFound, ref)
}

// weightRejected answers a rejected manual weigh-in with a readable page
// instead of a bare status code. The form is a plain HTML POST, so a bodyless
// 4xx renders as a blank white screen on a phone with no hint of what went
// wrong or that nothing was saved.
func weightRejected(c *gin.Context, status int, msg string) {
	back := c.Request.Referer()
	if back == "" {
		back = "/"
	}
	c.Data(status, "text/html; charset=utf-8", []byte(
		`<!doctype html><html><head><meta charset="utf-8">`+
			`<meta name="viewport" content="width=device-width,initial-scale=1">`+
			`<title>Weight not saved</title></head>`+
			`<body style="font-family:system-ui,sans-serif;padding:1.5rem;background:#1a1a1a;color:#eee">`+
			`<h1 style="font-size:1.25rem">Weight not saved</h1>`+
			`<p>`+html.EscapeString(msg)+`</p>`+
			`<p><a style="color:#6cb4ff" href="`+html.EscapeString(back)+`">Go back</a></p>`+
			`</body></html>`))
}

func deleteWeightHandler(c *gin.Context) {
	id, ok := formInt(c.PostForm("id"))
	if !ok {
		c.Status(http.StatusBadRequest)
		return
	}

	if err := dataStore.DeleteW(c.Request.Context(), id); err != nil {
		slog.Error("deleteWeightHandler: DeleteW failed",
			slog.Int("id", id), slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	ref := c.Request.Referer()
	if ref == "" {
		ref = "/stats/"
	}
	c.Redirect(http.StatusFound, ref)
}
