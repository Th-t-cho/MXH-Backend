package main

import (
	"core/app"
	"core/router"
)

// @title MXH Backend API
// @version 1.0
// @description API documentation for MXH backend.
// @host localhost:6969
// @BasePath /
// @schemes http
func main() {
	app.Setup()
	router.Setup()
}
