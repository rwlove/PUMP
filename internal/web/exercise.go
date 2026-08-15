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
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

// cvProxyClient serves the admin panel's live-data proxy. pump-cv answers
// these from memory, so a stalled sidecar should fail fast instead of
// hanging the request (http.DefaultClient has no timeout).
var cvProxyClient = &http.Client{Timeout: 30 * time.Second}

// cvUploadClient covers reference-clip uploads, which run pose extraction
// over the whole clip before responding — generous but still bounded.
var cvUploadClient = &http.Client{Timeout: 5 * time.Minute}

func exerciseHandler(c *gin.Context) {
	cfg := conf.Get()

	exs, ok := selectExOr500(c, "exerciseHandler")
	if !ok {
		return
	}
	sortExsByRecency(exs, lastPerformed(c, "exerciseHandler"))

	muscles := selectMusclesSoft(c, "exerciseHandler")

	var guiData models.GuiData
	guiData.Config = cfg
	guiData.ExData.Exs = exs
	guiData.GroupMap = orderedGroups(selectGroupsSoft(c, "exerciseHandler"), exs, muscles)
	guiData.Muscles = muscles

	idStr, ok := c.GetQuery("id")
	switch {
	case ok && idStr != "new":
		// Editing an existing exercise: load it.
		id, _ := strconv.Atoi(idStr)
		for _, oneEx := range exs {
			if oneEx.ID == id {
				guiData.OneEx = oneEx
				break
			}
		}
	case ok && idStr == "new":
		// Creating a new exercise. The group is chosen on the workout page and
		// carried here as ?group=, so the form shows it read-only rather than
		// asking. Pre-fill the color with this group's next free shade so the
		// picker shows a real color instead of black — the athlete can still
		// override it. (Feature 1 + Feature 3.)
		group := c.Query("group")
		guiData.OneEx.Group = group
		if group != "" {
			guiData.OneEx.Color = nextExerciseColor(group, exercisesInGroup(exs, group))
		}
	}

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
	oneEx.Voltra = c.PostForm("voltra") == "on"
	oneEx.Focus = c.PostForm("focus")
	oneEx.Bodyweight = c.PostForm("bodyweight") == "on"

	var ok bool
	if oneEx.ID, ok = formInt(c.PostForm("id")); !ok {
		c.Status(http.StatusBadRequest)
		return
	}
	if oneEx.Weight, ok = formDecimal(c.PostForm("weight")); !ok {
		c.Status(http.StatusBadRequest)
		return
	}
	if oneEx.Reps, ok = formInt(c.PostForm("reps")); !ok {
		c.Status(http.StatusBadRequest)
		return
	}

	// Auto-assign a color if none was provided: a shade from this exercise's
	// muscle-group band, so it reads as part of the group on the workout page.
	if oneEx.Color == "" {
		if exs, err := dataStore.SelectEx(c.Request.Context()); err == nil {
			oneEx.Color = nextExerciseColor(oneEx.Group, exercisesInGroup(exs, oneEx.Group))
		}
	}

	slog.Debug("saveExerciseHandler", slog.String("name", oneEx.Name), slog.Int("id", oneEx.ID))

	// Upsert: update in place when the exercise exists (id preserved, so
	// nothing referencing it churns); insert when new or the id is unknown.
	if oneEx.ID != 0 {
		found, err := dataStore.UpdateEx(c.Request.Context(), oneEx)
		if err != nil {
			slog.Error("saveExerciseHandler: UpdateEx failed", slog.Any("error", err))
			c.Status(http.StatusInternalServerError)
			return
		}
		if found {
			c.Redirect(http.StatusFound, "/")
			return
		}
	}

	if err := dataStore.InsertEx(c.Request.Context(), oneEx); err != nil {
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

	resp, err := cvProxyClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
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
	if !conf.Get().CVAutoLog {
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

	exs, err := dataStore.SelectEx(c.Request.Context())
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

	resp, err := cvUploadClient.Do(req)
	if err != nil {
		slog.Error("uploadReferenceClipHandler: pump-cv unreachable",
			slog.String("url", url), slog.Any("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
}

func deleteExerciseHandler(c *gin.Context) {
	id, ok := formInt(c.PostForm("id"))
	if !ok {
		c.Status(http.StatusBadRequest)
		return
	}

	if err := dataStore.DeleteEx(c.Request.Context(), id); err != nil {
		slog.Error("deleteExerciseHandler: DeleteEx failed",
			slog.Int("id", id), slog.Any("error", err))
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, "/")
}
