package web

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// adminHandler renders the CV tuning + debugging admin panel. Standalone
// page (no shared header) — designed for the touchscreen kiosk in
// landscape, but also usable from a desktop browser. All the live data
// it shows comes from pump-cv via PUMP_CV_URL; if pump-cv is down the
// page degrades gracefully (each tab shows its own "unavailable" state).
func adminHandler(c *gin.Context) {
	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.Version = Version
	c.HTML(http.StatusOK, "admin.html", guiData)
}
