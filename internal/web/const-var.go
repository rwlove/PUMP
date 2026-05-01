package web

import (
	"embed"

	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// Version is injected at build time via:
//
//	go build -ldflags "-X github.com/rwlove/PUMP/internal/web.Version=v0.7"
//
// Falls back to "dev" when building without ldflags.
var Version = "dev"

var (
	// appConfig - config for Web Gui
	appConfig models.Conf

	// Exercise data
	exData models.AllExData

	// dataStore is the active data source.
	// SQLiteStore in monolith mode, APIClient in split-frontend mode.
	dataStore store.Store

	// apiClient is non-nil only in split-frontend mode; used for config
	// operations that fall outside the Store interface.
	apiClient *store.APIClient
)

// templFS - html templates
//
//go:embed templates/*
var templFS embed.FS

// pubFS - public folder
//
//go:embed public/*
var pubFS embed.FS
