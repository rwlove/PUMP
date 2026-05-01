package main

import (
	_ "time/tzdata"

	"github.com/rwlove/PUMP/internal/api"
)

func main() {
	api.Start()
}
