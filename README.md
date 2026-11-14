# Claude

Projet Go.

## Structure

```
.
├── cmd/claude/        # point d'entrée de l'application (package main)
├── internal/greeting/ # code interne, non importable hors du module
├── go.mod
└── Makefile
```

## Commandes

```sh
make build   # compile dans bin/claude
make run     # lance l'application
make test    # lance les tests
make fmt     # formate le code
make vet     # analyse statique
```

Exemple :

```sh
go run ./cmd/claude -name Fred
# Hello, Fred!
```
