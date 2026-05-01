package web

import (
	"log"
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
		log.Println("ERROR addWeightHandler InsertW:", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, c.Request.Header["Referer"][0])
}

func weightHandler(c *gin.Context) {
	var guiData models.GuiData

	if idStr, ok := c.GetQuery("del"); ok {
		id, _ := strconv.Atoi(idStr)
		if err := dataStore.DeleteW(id); err != nil {
			log.Println("ERROR weightHandler DeleteW:", err)
		}
	}

	weights, err := dataStore.SelectW()
	if err != nil {
		log.Println("ERROR weightHandler SelectW:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	exData.Weight = weights

	guiData.Config = appConfig
	guiData.ExData = exData

	sort.Slice(guiData.ExData.Weight, func(i, j int) bool {
		return guiData.ExData.Weight[i].Date < guiData.ExData.Weight[j].Date
	})

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "weight.html", guiData)
}
