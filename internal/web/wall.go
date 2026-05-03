package web

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// wallHandler renders the kiosk-mode wall view. Standalone template
// (no shared header/footer) — wall is a fixed-layout dashboard, not
// part of the regular browse experience.
func wallHandler(c *gin.Context) {
	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.Version = Version
	c.HTML(http.StatusOK, "wall.html", guiData)
}
