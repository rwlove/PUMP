package web

import (
	"embed"

	"github.com/rwlove/PUMP/internal/store"
)

// Version is injected at build time via:
//
//	go build -ldflags "-X github.com/rwlove/PUMP/internal/web.Version=v0.7"
//
// Falls back to "dev" when building without ldflags.
var Version = "dev"

var (
	// dataStore is the active data source.
	dataStore store.Store

	// healthStore persists and serves wearable health records.
	healthStore store.HealthStore
)

// templFS - html templates
//
//go:embed templates/*
var templFS embed.FS

// pubFS - public folder
//
//go:embed public/*
var pubFS embed.FS
