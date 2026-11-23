// Package requestid lleva el identificador de correlación de una petición dentro del
// context, para que lo usen los logs, los errores y los eventos sin pasarlo a mano.
package requestid

import "context"

// Header es la cabecera HTTP donde viaja el identificador.
const Header = "X-Request-Id"

type ctxKey struct{}

func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From devuelve el identificador del context, o "" si no hay.
func From(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Valid acepta de 1 a 64 caracteres ASCII visibles. Así un cliente no puede
// meter saltos de línea ni textos enormes en los logs.
func Valid(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}
