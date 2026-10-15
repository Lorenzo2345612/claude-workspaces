//go:build terminal

package main

import "errors"

// appDo: the terminal version has no app server.
func appDo(q map[string]any) (map[string]any, error) {
	return nil, errors.New("esta versión de cw es solo de terminal: no tiene la app")
}
