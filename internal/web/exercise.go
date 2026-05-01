package web

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

func exerciseHandler(c *gin.Context) {
	var guiData models.GuiData
	var id int

	exs, err := dataStore.SelectEx()
	if err != nil {
		log.Println("ERROR exerciseHandler SelectEx:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	sets, err := dataStore.SelectSet()
	if err != nil {
		log.Println("ERROR exerciseHandler SelectSet:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	exData.Exs = exs

	guiData.Config = appConfig
	guiData.ExData = exData
	guiData.GroupMap = createGroupMap()

	sortExsByFrequency(guiData.ExData.Exs, sets, appConfig.FrequencyDays)

	idStr, ok := c.GetQuery("id")
	if ok && idStr != "new" {
		id, _ = strconv.Atoi(idStr)
		for _, oneEx := range exData.Exs {
			if oneEx.ID == id {
				guiData.OneEx = oneEx
				break
			}
		}
	}

	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "exercise.html", guiData)
}

func saveExerciseHandler(c *gin.Context) {
	var oneEx models.Exercise

	oneEx.Group = c.PostForm("group")
	oneEx.Place = c.PostForm("place")
	oneEx.Name = c.PostForm("name")
	oneEx.Descr = c.PostForm("descr")
	oneEx.Image = c.PostForm("image")
	oneEx.Color = c.PostForm("color")

	oneEx.ID, _ = strconv.Atoi(c.PostForm("id"))
	oneEx.Weight, _ = decimal.NewFromString(c.PostForm("weight"))
	oneEx.Reps, _ = strconv.Atoi(c.PostForm("reps"))

	// Auto-assign a distinct color if none was provided.
	if oneEx.Color == "" {
		if exs, err := dataStore.SelectEx(); err == nil {
			oneEx.Color = nextExerciseColor(collectColors(exs))
		}
	}

	log.Println("ONEEX =", oneEx)

	// Upsert: delete the old record first (ID=0 means new exercise, skip delete)
	if oneEx.ID != 0 {
		if err := dataStore.DeleteEx(oneEx.ID); err != nil {
			log.Println("ERROR saveExerciseHandler DeleteEx:", err)
		}
		// Clear ID so the store inserts a new row (SQLite auto-increment);
		// for the API client InsertEx with ID!=0 does a PUT instead.
		// We keep ID here so APIClient.InsertEx routes to PUT /api/exercises/:id.
	}

	if err := dataStore.InsertEx(oneEx); err != nil {
		log.Println("ERROR saveExerciseHandler InsertEx:", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, "/")
}

func deleteExerciseHandler(c *gin.Context) {
	id, _ := strconv.Atoi(c.PostForm("id"))

	if err := dataStore.DeleteEx(id); err != nil {
		log.Println("ERROR deleteExerciseHandler:", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, "/")
}
