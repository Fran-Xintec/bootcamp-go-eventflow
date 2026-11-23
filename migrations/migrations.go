// Package migrations embebe los archivos SQL para que viajen dentro del binario
// y no dependan de la carpeta desde donde se ejecuta la aplicación.
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
