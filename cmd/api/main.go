package main

import (
	"log"

	"eventflow/cmd/app"
)

func main() {
	app := app.NewApp()

	if err := app.Init(); err != nil {
		log.Fatal("Error al iniciar: ", err)
	}

	if err := app.Run(); err != nil {
		log.Fatal("Error al ejecutar la aplicación: ", err)
	}
}
