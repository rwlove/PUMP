package web

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

func setHandler(c *gin.Context) {
	var formData []models.Set
	var oneSet models.Set

	_ = c.PostFormMap("sets")
	formMap := c.Request.PostForm

	formLen := len(formMap["name"])
	if formLen == 0 {
		c.Status(http.StatusOK)
		return
	}
	date := formMap["date"][0]

	for i := 0; i < formLen; i++ {
		oneSet.Date = date
		oneSet.Name = formMap["name"][i]
		oneSet.Weight, _ = decimal.NewFromString(formMap["weight"][i])
		oneSet.Reps, _ = strconv.Atoi(formMap["reps"][i])
		if wc, ok := formMap["workout_color"]; ok && i < len(wc) {
			oneSet.WorkoutColor = wc[i]
		} else {
			oneSet.WorkoutColor = "#2780e3"
		}
		if n, ok := formMap["note"]; ok && i < len(n) {
			oneSet.Note = n[i]
		}
		formData = append(formData, oneSet)
	}

	if err := dataStore.BulkReplaceSetsByDate(date, formData); err != nil {
		slog.Error("setHandler: BulkReplaceSetsByDate failed",
			slog.String("date", date), slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}
