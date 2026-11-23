package app

import "eventflow/pkg/logger"

// Shutdown cierra los recursos globales. Run lo llama al final, cuando ya no
// quedan peticiones en curso (o venció el plazo).
func (a *App) Shutdown() {
	logger.General("Cerrando conexiones y recursos...")

	if a.container != nil {
		a.container.Publisher.Close()
	}

	if a.db != nil {
		a.db.Close()
	}

	logger.General("Recursos cerrados correctamente")
}
