package web

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// wallHandler renders the kiosk-mode wall view. Now uses the standard
// header so the AI page is consistent with the rest of the app's nav;
// the kiosk layout renders below the navbar.
func wallHandler(c *gin.Context) {
	var guiData models.GuiData
	guiData.Config = appConfig
	guiData.Version = Version
	c.HTML(http.StatusOK, "header.html", guiData)
	c.HTML(http.StatusOK, "wall.html", guiData)
}
