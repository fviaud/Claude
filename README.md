# Claude

API HTTP en Go pour gérer un catalogue d'articles (catégories, prix, stock),
stocké dans PostgreSQL.

## Structure

```
.
├── cmd/claude/        # point d'entrée : connexion, migrations, serveur HTTP
├── internal/
│   ├── catalogue/     # accès aux tables catalogue et categories
│   ├── database/      # pool de connexions PostgreSQL (pgx) et migrations
│   │   └── migrations/  # fichiers SQL appliqués au démarrage
│   └── httpapi/       # routes et handlers JSON
├── docker-compose.yml # PostgreSQL 16 pour le développement local
├── .env.example       # variables d'environnement à copier dans .env
├── go.mod
└── Makefile
```

## Configuration

| Variable       | Défaut  | Rôle                                   |
|----------------|---------|----------------------------------------|
| `DATABASE_URL` | —       | connexion PostgreSQL (obligatoire)     |
| `ADDR`         | `:8080` | adresse d'écoute de l'API              |

## Base de données

L'application se connecte à PostgreSQL via la variable `DATABASE_URL`
(format `postgres://user:password@host:5432/dbname?sslmode=disable`).

```sh
cp .env.example .env   # le Makefile charge .env automatiquement
make db-up             # démarre PostgreSQL avec Docker
make run               # applique les migrations et démarre l'API sur :8080
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

## API

Toutes les requêtes et réponses sont en JSON. Les erreurs ont la forme
`{"error": "message"}`.

| Méthode  | Route                  | Rôle                                             |
|----------|------------------------|--------------------------------------------------|
| `GET`    | `/healthz`             | état du service et de la base                    |
| `GET`    | `/items`               | liste (`?limit=50&offset=0&category_id=1`)       |
| `POST`   | `/items`               | crée un article                                  |
| `GET`    | `/items/{id}`          | lit un article                                   |
| `PUT`    | `/items/{id}`          | remplace un article (tous les champs)            |
| `DELETE` | `/items/{id}`          | supprime un article                              |
| `POST`   | `/items/{id}/stock`    | ajoute ou retire du stock : `{"delta": -2}`      |
| `GET`    | `/categories`          | liste les catégories                             |
| `POST`   | `/categories`          | crée une catégorie : `{"name": "Meubles"}`       |
| `DELETE` | `/categories/{id}`     | supprime une catégorie (ses articles restent)    |

Corps de `POST /items` et `PUT /items/{id}` :

```json
{
  "reference": "C1",
  "name": "Chaise",
  "description": "En bois",
  "price_cents": 4999,
  "category_id": 1,
  "stock": 5
}
```

`reference` et `name` sont obligatoires ; `category_id` peut être `null`.

Codes de retour : `200`/`201`/`204` succès, `400` requête mal formée,
`404` introuvable, `409` conflit (référence ou catégorie déjà existante,
stock insuffisant), `422` données invalides, `503` base indisponible.

Exemple :

```sh
curl -X POST localhost:8080/categories -d '{"name":"Meubles"}'
curl -X POST localhost:8080/items \
  -d '{"reference":"C1","name":"Chaise","price_cents":4999,"stock":5,"category_id":1}'
curl -X POST localhost:8080/items/1/stock -d '{"delta":-2}'
curl 'localhost:8080/items?category_id=1'
```

## Commandes

```sh
make build             # compile dans bin/claude
make run               # lance l'API
make test              # tests unitaires
make test-integration  # tests avec une vraie base (DATABASE_URL)
make fmt               # formate le code
make vet               # analyse statique
make db-up / db-down   # démarre / arrête PostgreSQL avec Docker
```
