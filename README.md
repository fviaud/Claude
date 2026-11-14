# Claude

Projet Go.

## Structure

```
.
├── cmd/claude/        # point d'entrée de l'application (package main)
├── internal/
│   ├── database/      # pool de connexions PostgreSQL (pgx)
│   └── greeting/      # code d'exemple
├── docker-compose.yml # PostgreSQL 16 pour le développement local
├── .env.example       # variables d'environnement à copier dans .env
├── go.mod
└── Makefile
```

## Base de données

L'application se connecte à PostgreSQL via la variable `DATABASE_URL`
(format `postgres://user:password@host:5432/dbname?sslmode=disable`).

```sh
cp .env.example .env   # le Makefile charge .env automatiquement
make db-up             # démarre PostgreSQL avec Docker
make run               # Hello, world! / Connected to PostgreSQL 16...
make db-down           # arrête PostgreSQL
```

## Commandes

```sh
make build   # compile dans bin/claude
make run     # lance l'application
make test    # lance les tests unitaires
make test-integration  # tests avec une vraie base (DATABASE_URL)
make fmt     # formate le code
make vet     # analyse statique
```

Exemple :

```sh
DATABASE_URL=postgres://claude:claude@localhost:5432/claude?sslmode=disable \
  go run ./cmd/claude -name Fred
# Hello, Fred!
# Connected to PostgreSQL 16...
```
