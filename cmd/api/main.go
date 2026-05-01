package main

import (
	"os"
	_ "time/tzdata"

	"github.com/rwlove/PUMP/internal/api"
	"github.com/rwlove/PUMP/internal/logger"
)

func main() {
	logger.Init(os.Getenv("LOG_LEVEL"))
	api.Start()
}
