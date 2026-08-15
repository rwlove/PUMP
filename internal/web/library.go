package web

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/conf"
	"github.com/rwlove/PUMP/internal/models"
)

// libraryHandler renders the exercise-management surface. Defining exercises
// (create / edit / delete) lives here, off the logging page: the workout page
// only selects existing exercises to log. The template groups exercises by
// group and offers a per-group "New" plus a per-row link into the editor.
func libraryHandler(c *gin.Context) {
	cfg := conf.Get()

	exs, ok := selectExOr500(c, "libraryHandler")
	if !ok {
		return
	}
	muscles := selectMusclesSoft(c, "libraryHandler")

	// Stable management order: within a group, by Place then Name. The template
	// iterates GroupMap and filters, so exs only needs a deterministic order.
	sort.SliceStable(exs, func(i, j int) bool {
		if exs[i].Place != exs[j].Place {
			return exs[i].Place < exs[j].Place
		}
		return exs[i].Name < exs[j].Name
	})

	groups := selectGroupsSoft(c, "libraryHandler")

	var guiData models.GuiData
	guiData.Config = cfg
	guiData.ExData.Exs = exs
	// orderedGroups so a group defined only in the muscle catalog — no exercises
	// yet — still shows with its own "New" button, and managed groups appear in
	// their configured order.
	guiData.GroupMap = orderedGroups(groups, exs, muscles)
	guiData.Muscles = muscles
	guiData.Groups = groups
	guiData.Routines = selectRoutinesSoft(c, "libraryHandler")
	guiData.Version = Version

	c.HTML(http.StatusOK, "library.html", guiData)
}
