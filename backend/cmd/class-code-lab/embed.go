package main

import "embed"

// frontendFS embeds the built SPA so the portable exe can serve the UI
// without external files. The directory is populated by build-portable.sh
// (a copy of frontend/dist). In dev, FRONTEND_DIST in backend/.env points
// to the on-disk dist for hot-reload; when FRONTEND_DIST is empty the
// binary serves from this embed.FS instead.
//
//go:embed all:frontend-dist
var frontendFS embed.FS
