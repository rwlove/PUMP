package web

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

func setHandler(c *gin.Context) {
	var formData []models.Set
	var oneSet models.Set

	// Force form parsing so c.Request.PostForm is populated below (mirrors
	// what gin's PostForm accessors do internally).
	_ = c.Request.ParseMultipartForm(32 << 20)
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
		var ok bool
		if oneSet.Weight, ok = formDecimal(formMap["weight"][i]); !ok {
			c.Status(http.StatusBadRequest)
			return
		}
		if oneSet.Reps, ok = formInt(formMap["reps"][i]); !ok {
			c.Status(http.StatusBadRequest)
			return
		}
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

	if err := dataStore.BulkReplaceSetsByDate(c.Request.Context(), date, formData); err != nil {
		slog.Error("setHandler: BulkReplaceSetsByDate failed",
			slog.String("date", date), slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}
