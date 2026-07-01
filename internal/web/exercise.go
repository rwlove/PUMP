package web

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

func exerciseHandler(c *gin.Context) {
	exs, ok := selectExOr500(c, "exerciseHandler")
	if !ok {
		return
	}
	sets, ok := selectSetsOr500(c, "exerciseHandler")
	if !ok {
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

// pumpCVProxyHandler forwards any /api/cv/* request to pump-cv. The
// path after /api/cv/ becomes the path on pump-cv. Method, body, and
// most headers are passed through. Used by the admin panel to reach
// pump-cv's live data (state, prototypes, snapshots, metrics) without
// exposing pump-cv directly to the kiosk's network.
//
// PUMP_CV_URL must be set; if not, returns 503.
func pumpCVProxyHandler(c *gin.Context) {
	cvURL := os.Getenv("PUMP_CV_URL")
	if cvURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PUMP_CV_URL is not configured"})
		return
	}
	// Path after the /api/cv/ prefix, preserved verbatim.
	subpath := c.Param("path")
	url := strings.TrimRight(cvURL, "/") + subpath
	if c.Request.URL.RawQuery != "" {
		url += "?" + c.Request.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(c.Request.Context(),
		c.Request.Method, url, c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Pass through Content-Type for POST/PUT bodies.
	if ct := c.Request.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	// Server-side auth to pump-cv. The browser never carries this — pump
	// and pump-cv share API_KEY / PUMP_API_KEY (same 1Password field), so
	// pump signs the upstream call on the kiosk's behalf.
	if k := os.Getenv("API_KEY"); k != "" {
		req.Header.Set("X-Api-Key", k)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

// uploadReferenceClipHandler accepts a video clip uploaded from the
// exercise edit page and forwards it to pump-cv for prototype
// extraction. The exercise name is looked up server-side from the path
// :id so the browser doesn't have to send it (and can't lie about it).
//
// PUMP_CV_URL must be set; if not, this returns 503. CVAutoLog must be
// on; if not, returns 412 — a polite "you have to flip the toggle in
// settings first."
func uploadReferenceClipHandler(c *gin.Context) {
	if !appConfig.CVAutoLog {
		c.JSON(http.StatusPreconditionFailed,
			gin.H{"error": "CV auto-log is disabled"})
		return
	}
	cvURL := os.Getenv("PUMP_CV_URL")
	if cvURL == "" {
		c.JSON(http.StatusServiceUnavailable,
			gin.H{"error": "PUMP_CV_URL is not configured"})
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	exs, err := dataStore.SelectEx()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var exerciseName string
	for _, e := range exs {
		if e.ID == id {
			exerciseName = e.Name
			break
		}
	}
	if exerciseName == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "exercise not found"})
		return
	}

	clip, header, err := c.Request.FormFile("clip")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing clip file"})
		return
	}
	defer clip.Close()

	// Re-stream the upload as multipart to pump-cv with the exercise
	// name added. Using bytes.Buffer over io.Pipe to keep error handling
	// simple; reference clips are short (a few MB).
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("exercise_name", exerciseName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	part, err := w.CreateFormFile("clip", header.Filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := io.Copy(part, clip); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	w.Close()

	url := fmt.Sprintf("%s/api/v1/reference", cvURL)
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", url, &body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if k := os.Getenv("API_KEY"); k != "" {
		req.Header.Set("X-Api-Key", k)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("uploadReferenceClipHandler: pump-cv unreachable",
			slog.String("url", url), slog.Any("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
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
