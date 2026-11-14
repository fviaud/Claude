// Command claude is the entry point of the application.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fviaud/claude/internal/catalogue"
	"github.com/fviaud/claude/internal/database"
	"github.com/fviaud/claude/internal/greeting"
)

func main() {
	name := flag.String("name", "world", "name to greet")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *name); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, name string) error {
	fmt.Println(greeting.Hello(name))

	pool, err := database.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	version, err := database.ServerVersion(ctx, pool)
	if err != nil {
		return err
	}
	fmt.Println("Connected to", version)

	applied, err := database.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	for _, m := range applied {
		fmt.Println("Applied migration", m)
	}

	count, err := catalogue.NewRepository(pool).Count(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Catalogue: %d item(s)\n", count)

	return nil
}
