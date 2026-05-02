package web

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

func exerciseHandler(c *gin.Context) {
	exs, err := dataStore.SelectEx()
	if err != nil {
		slog.Error("exerciseHandler: SelectEx failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}
	sets, err := dataStore.SelectSet()
	if err != nil {
		slog.Error("exerciseHandler: SelectSet failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	sortExsByFrequency(exs, sets, appConfig.FrequencyDays)

	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.ExData.Exs = exs
	guiData.GroupMap = buildGroupList(exs)

	idStr, ok := c.GetQuery("id")
	if ok && idStr != "new" {
		id, _ := strconv.Atoi(idStr)
		for _, oneEx := range exs {
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

	slog.Debug("saveExerciseHandler", slog.String("name", oneEx.Name), slog.Int("id", oneEx.ID))

	// Upsert: delete the old record first (ID=0 means new exercise, skip delete)
	if oneEx.ID != 0 {
		if err := dataStore.DeleteEx(oneEx.ID); err != nil {
			slog.Warn("saveExerciseHandler: DeleteEx failed (continuing)",
				slog.Int("id", oneEx.ID), slog.Any("error", err))
		}
	}

	if err := dataStore.InsertEx(oneEx); err != nil {
		slog.Error("saveExerciseHandler: InsertEx failed", slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, "/")
}

func deleteExerciseHandler(c *gin.Context) {
	id, _ := strconv.Atoi(c.PostForm("id"))

	if err := dataStore.DeleteEx(id); err != nil {
		slog.Error("deleteExerciseHandler: DeleteEx failed",
			slog.Int("id", id), slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, "/")
}
