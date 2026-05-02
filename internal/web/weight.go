package web

import (
	"log/slog"
	"net/http"
	"sort"
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
		ref = "/weight/"
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

	c.Redirect(http.StatusFound, "/weight/")
}

func weightHandler(c *gin.Context) {
	weights, err := dataStore.SelectW()
	if err != nil {
		slog.Error("weightHandler: SelectW failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	sort.Slice(weights, func(i, j int) bool {
		return weights[i].Date < weights[j].Date
	})

	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.ExData.Weight = weights

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "weight.html", guiData)
}
