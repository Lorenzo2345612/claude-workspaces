//go:build terminal

package main

import "errors"

// appBuild tells which package this binary comes from: the terminal-only one has no app.
const appBuild = false

func cmdApp([]string) error {
	return errors.New("esta instalación de cw es solo de terminal; para la app de escritorio instala «Agent Workspaces»")
}
