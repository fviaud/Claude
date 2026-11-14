// Package greeting builds greeting messages.
package greeting

import "fmt"

// Hello returns a greeting for name. An empty name falls back to "world".
func Hello(name string) string {
	if name == "" {
		name = "world"
	}
	return fmt.Sprintf("Hello, %s!", name)
}
