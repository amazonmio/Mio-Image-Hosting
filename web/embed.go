package web

import "embed"

// Build the Vue frontend before compiling the production binary.
//
//go:embed all:dist*
var Files embed.FS

//go:embed public/logo.webp
var DefaultAvatar []byte
