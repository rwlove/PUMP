package web

import (
	"log"
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
	date := formMap["date"][0]

	for i := 0; i < formLen; i++ {
		oneSet.Date = date
		oneSet.Name = formMap["name"][i]
		oneSet.Weight, _ = decimal.NewFromString(formMap["weight"][i])
		oneSet.Reps, _ = strconv.Atoi(formMap["reps"][i])
		if wc, ok := formMap["workout_color"]; ok && i < len(wc) {
			oneSet.WorkoutColor = wc[i]
		} else {
			oneSet.WorkoutColor = "#03a70c"
		}
		formData = append(formData, oneSet)
	}

	if err := dataStore.BulkReplaceSetsByDate(date, formData); err != nil {
		log.Println("ERROR setHandler BulkReplaceSetsByDate:", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, "/")
}
