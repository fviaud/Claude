// Command claude is the entry point of the application.
package main

import (
	"flag"
	"fmt"

	"github.com/fviaud/claude/internal/greeting"
)

func main() {
	name := flag.String("name", "world", "name to greet")
	flag.Parse()

	fmt.Println(greeting.Hello(*name))
}
