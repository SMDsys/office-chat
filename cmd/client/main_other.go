//go:build !windows

package main

import "fmt"

// Клиент собирается только под Windows; здесь заглушка, чтобы `go build ./...`
// и проверки работали на других системах.
func main() {
	fmt.Println("Тонкий клиент Офис-чата собирается только для Windows.")
}
