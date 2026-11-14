# Claude

Projet Go.

## Structure

```
.
├── cmd/claude/        # point d'entrée de l'application (package main)
├── internal/
│   ├── catalogue/     # table catalogue : création, lecture, mise à jour, suppression
│   ├── database/      # pool de connexions PostgreSQL (pgx) et migrations
│   │   └── migrations/  # fichiers SQL appliqués au démarrage
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

### Migrations

Au démarrage, l'application applique les fichiers de
`internal/database/migrations/` qui ne l'ont pas encore été, dans l'ordre de
leur nom, et les enregistre dans la table `schema_migrations`. Pour faire
évoluer le schéma, ajouter un fichier numéroté (`0002_....sql`) ; ne jamais
modifier un fichier déjà appliqué.

### Table `catalogue`

| Colonne       | Type          | Remarque                         |
|---------------|---------------|----------------------------------|
| `id`          | `BIGINT`      | clé primaire auto-incrémentée    |
| `reference`   | `TEXT`        | obligatoire, unique              |
| `name`        | `TEXT`        | obligatoire, indexé              |
| `description` | `TEXT`        | vide par défaut                  |
| `price_cents` | `BIGINT`      | prix en centimes, ≥ 0            |
| `category_id` | `BIGINT`      | catégorie (facultative), → `categories.id` |
| `stock`       | `INTEGER`     | quantité disponible, ≥ 0, 0 par défaut |
| `created_at`  | `TIMESTAMPTZ` | date de création                 |
| `updated_at`  | `TIMESTAMPTZ` | date de dernière modification    |

### Table `categories`

| Colonne      | Type          | Remarque                      |
|--------------|---------------|-------------------------------|
| `id`         | `BIGINT`      | clé primaire auto-incrémentée |
| `name`       | `TEXT`        | obligatoire, unique           |
| `created_at` | `TIMESTAMPTZ` | date de création              |

Supprimer une catégorie conserve ses articles, qui n'ont alors plus de catégorie.
Le stock se modifie avec `AdjustStock`, qui refuse de le rendre négatif.

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
# Applied migration 0001_create_catalogue
# Catalogue: 0 item(s)
```
