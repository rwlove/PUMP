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

	// The date is what BulkReplaceSetsByDate keys on. Without it there is no
	// target for the replace, so reject rather than touch the database. This
	// also guards the indexing below, which is why the empty-form check keys
	// on the date and not on the set count.
	if len(formMap["date"]) == 0 || formMap["date"][0] == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	date := formMap["date"][0]

	// A zero-length set list is legitimate: it means the user deleted every
	// set for this date. Falling through with an empty formData clears the
	// day. Short-circuiting here instead would strand the final set, since
	// the autosave POST that follows the last delete carries no name fields.
	formLen := len(formMap["name"])

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
