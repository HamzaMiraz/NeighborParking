package httpapi

import "embed"

//go:embed web/*
var frontend embed.FS
