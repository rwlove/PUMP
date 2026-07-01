package web

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// The select*Or500 helpers wrap the store reads every page handler starts
// with: on failure they log with the handler's name and write a 500, so
// callers just bail when ok=false.

func selectExOr500(c *gin.Context, who string) ([]models.Exercise, bool) {
	exs, err := dataStore.SelectEx(c.Request.Context())
	if err != nil {
		slog.Error(who+": SelectEx failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return nil, false
	}
	return exs, true
}

func selectSetsOr500(c *gin.Context, who string) ([]models.Set, bool) {
	sets, err := dataStore.SelectSet(c.Request.Context())
	if err != nil {
		slog.Error(who+": SelectSet failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return nil, false
	}
	return sets, true
}

func selectWeightsOr500(c *gin.Context, who string) ([]models.BodyWeight, bool) {
	weights, err := dataStore.SelectW(c.Request.Context())
	if err != nil {
		slog.Error(who+": SelectW failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return nil, false
	}
	return weights, true
}
