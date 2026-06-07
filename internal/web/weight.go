package web

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

func addWeightHandler(c *gin.Context) {
	var w models.BodyWeight

	w.Date = c.PostForm("date")
	w.Weight, _ = decimal.NewFromString(c.PostForm("weight"))

	if err := dataStore.InsertW(w); err != nil {
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
	id, _ := strconv.Atoi(c.PostForm("id"))

	if err := dataStore.DeleteW(id); err != nil {
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
