package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

// ─── measurements (grip, dead hang) ─────────────────────────────────────────────

func getMeasurements(c *gin.Context) {
	ms, err := dataStore.SelectMeasurements(c.Request.Context())
	if err != nil {
		slog.Error("getMeasurements: SelectMeasurements failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ms)
}

// postMeasurement logs a grip (left/right, lb) and/or a dead-hang (seconds)
// reading for a date. Any provided field is inserted; omitted or blank ones are
// skipped, so you can capture whatever you measured that day.
func postMeasurement(c *gin.Context) {
	var body struct {
		Date      string  `json:"Date"`
		GripLeft  *string `json:"GripLeft"`
		GripRight *string `json:"GripRight"`
		DeadHang  *string `json:"DeadHang"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	date := strings.TrimSpace(body.Date)
	if date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date is required"})
		return
	}

	type rec struct{ metric, hand, val string }
	var recs []rec
	add := func(metric, hand string, v *string) {
		if v == nil {
			return
		}
		if s := strings.TrimSpace(*v); s != "" {
			recs = append(recs, rec{metric, hand, s})
		}
	}
	add("grip", "left", body.GripLeft)
	add("grip", "right", body.GripRight)
	add("dead_hang", "", body.DeadHang)
	if len(recs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no values provided"})
		return
	}

	for _, r := range recs {
		val, err := decimal.NewFromString(r.val)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid number: " + r.val})
			return
		}
		if err := dataStore.InsertMeasurement(c.Request.Context(), models.Measurement{
			Date: date, Metric: r.metric, Hand: r.hand, Value: val,
		}); err != nil {
			slog.Error("postMeasurement: InsertMeasurement failed", slog.Any("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	slog.Info("measurements logged", slog.String("date", date), slog.Int("count", len(recs)))
	c.Status(http.StatusCreated)
}

func deleteMeasurement(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := dataStore.DeleteMeasurement(c.Request.Context(), id); err != nil {
		slog.Error("deleteMeasurement: DeleteMeasurement failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
