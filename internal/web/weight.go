package web

import (
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
		c.Status(http.StatusBadRequest)
		return
	}

	// Same plausibility guard the API scale path enforces. formDecimal maps a
	// blank field to zero, which is below the floor, so this also rejects an
	// accidental empty submit that would otherwise persist a 0-lb weigh-in and
	// poison every body-weight chart.
	if conf.WeightOutOfRange(w.Weight) {
		slog.Warn("addWeightHandler: rejected out-of-range weight",
			slog.String("date", w.Date), slog.String("weight", w.Weight.String()))
		c.Status(http.StatusUnprocessableEntity)
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
