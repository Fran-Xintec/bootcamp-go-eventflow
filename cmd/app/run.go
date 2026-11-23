package app

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"eventflow/pkg/logger"
)

func (a *App) Run() error {
	// Timeouts del servidor: sin ellos un cliente lento (o malicioso) puede
	// mantener conexiones abiertas para siempre.
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(a.config.Port),
		Handler:           a.router,
		ReadHeaderTimeout: 5 * time.Second,  // tiempo para recibir las cabeceras
		ReadTimeout:       10 * time.Second, // tiempo para recibir la petición completa
		WriteTimeout:      15 * time.Second, // tiempo para escribir la respuesta
		IdleTimeout:       60 * time.Second, // conexiones keep-alive sin uso
	}

	go func() {
		logger.Success("EventFlow iniciado correctamente en el puerto: " + strconv.Itoa(a.config.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Error al iniciar servidor: " + err.Error())
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	sig := <-quit
	logger.General("Terminando eventflow..." + sig.String())

	// Un solo plazo para todo el apagado.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(a.config.ShutDownTimeOut)*time.Second)
	defer cancel()

	// 1. Se dejan de aceptar conexiones nuevas y se esperan las peticiones en curso
	// (la publicación del evento ocurre dentro de la petición, así que también termina).
	var shutdownErr error
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Error al cerrar servidor: " + err.Error())
		shutdownErr = err
	}

	// 2. Se cierran los recursos (conexión a RabbitMQ y base de datos) aunque algo haya fallado.
	a.Shutdown()
	logger.L().Sync()

	if shutdownErr != nil {
		return shutdownErr
	}
	logger.Success("Servidor terminado correctamente")
	return nil
}
