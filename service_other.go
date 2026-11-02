//go:build !windows

package main

import (
	"errors"
	"log"
)

func isWindowsService() bool { return false }

func runAsService(_ Config, _ *log.Logger) error {
	return errors.New("режим службы доступен только на Windows")
}

func installService(_ string) error {
	return errors.New("установка службы доступна только на Windows")
}

func uninstallService() error {
	return errors.New("удаление службы доступно только на Windows")
}
