# Claude

API HTTP en Go pour gérer un catalogue de produits (catégories, prix, stock),
stocké dans PostgreSQL.

## Structure

```
.
├── cmd/claude/        # point d'entrée : connexion, migrations, serveur HTTP
├── .github/workflows/ # CI GitHub Actions
├── docs/
│   ├── openapi.yaml   # spécification OpenAPI 3 de l'API
│   └── schema.md      # schéma détaillé de la base de données
├── internal/
│   ├── catalogue/     # accès aux tables products et categories
│   ├── database/      # pool de connexions PostgreSQL (pgx) et migrations
│   │   └── migrations/  # fichiers SQL appliqués au démarrage
│   └── httpapi/       # routes et handlers JSON
├── Dockerfile         # image de production (binaire statique, distroless)
├── docker-compose.yml # PostgreSQL 16, et l'API avec le profil "app"
├── scripts/smoke-test.sh # test de l'image Docker en conditions réelles
├── .env.example       # variables d'environnement à copier dans .env
├── go.mod
└── Makefile
```

## Configuration

| Variable       | Défaut  | Rôle                                   |
|----------------|---------|----------------------------------------|
| `DATABASE_URL` | —       | connexion PostgreSQL (obligatoire)     |
| `ADDR`         | `:8080` | adresse d'écoute de l'API              |
| `API_KEYS`     | —       | clés d'API acceptées, séparées par des virgules (obligatoire) |

## Base de données

L'application se connecte à PostgreSQL via la variable `DATABASE_URL`
(format `postgres://user:password@host:5432/dbname?sslmode=disable`).

```sh
cp .env.example .env   # le Makefile charge .env automatiquement
make db-up             # démarre PostgreSQL avec Docker
make run               # applique les migrations et démarre l'API sur :8080
make db-down           # arrête PostgreSQL
make db-reset          # supprime la base locale (recréée au prochain démarrage)
```

### Migrations

Au démarrage, l'application applique les fichiers de
`internal/database/migrations/` qui ne l'ont pas encore été, dans l'ordre de
leur nom, et les enregistre dans la table `schema_migrations`. Pour faire
évoluer le schéma, ajouter un fichier numéroté (`0002_....sql`) ; ne jamais
modifier un fichier déjà appliqué.

Le schéma détaillé, avec diagramme et règles métier, est dans
[`docs/schema.md`](docs/schema.md).

### Table `products`

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

Supprimer une catégorie conserve ses produits, qui n'ont alors plus de catégorie.
Le stock se modifie avec `AdjustStock`, qui refuse de le rendre négatif.

## API

Toutes les requêtes et réponses sont en JSON. Les erreurs ont la forme
`{"error": "message"}`.

La spécification complète est dans [`docs/openapi.yaml`](docs/openapi.yaml)
(OpenAPI 3.0). Pour l'explorer :

- **Swagger UI** :
  `docker run -p 8081:8080 -e SWAGGER_JSON=/spec/openapi.yaml -v "$PWD/docs:/spec" swaggerapi/swagger-ui`
  puis ouvrir http://localhost:8081 ; le bouton *Authorize* permet de saisir la clé d'API.
- **Postman** ou **Insomnia** : *Import* → choisir le fichier `docs/openapi.yaml`.
- **En ligne** : coller le fichier dans https://editor.swagger.io.

Les tests vérifient que la spécification reste fidèle au code : chaque route
du serveur doit y figurer (et inversement), et chaque réponse réelle, y
compris d'erreur, doit respecter le format décrit. Modifier une route sans
mettre à jour `docs/openapi.yaml` fait échouer `make test`.

### Authentification

Toutes les routes sauf `/healthz` exigent une clé d'API, envoyée au choix :

```sh
curl -H 'X-API-Key: <clé>' localhost:8080/products
curl -H 'Authorization: Bearer <clé>' localhost:8080/products
```

Sans clé valide, l'API répond `401`. Les clés sont définies dans `API_KEYS`
(par exemple `API_KEYS=cle1,cle2`) ; en accepter plusieurs permet d'en changer
sans interruption : ajouter la nouvelle, mettre à jour les clients, retirer
l'ancienne. L'API refuse de démarrer si `API_KEYS` est vide.

Générer une clé : `openssl rand -hex 32`.

| Méthode  | Route                  | Rôle                                             |
|----------|------------------------|--------------------------------------------------|
| `GET`    | `/healthz`             | état du service et de la base                    |
| `GET`    | `/products`               | liste (`?limit=50&offset=0&category_id=1`)       |
| `POST`   | `/products`               | crée un produit                                  |
| `GET`    | `/products/{id}`          | lit un produit                                   |
| `PUT`    | `/products/{id}`          | remplace un produit (tous les champs)            |
| `DELETE` | `/products/{id}`          | supprime un produit                              |
| `POST`   | `/products/{id}/stock`    | ajoute ou retire du stock : `{"delta": -2}`      |
| `GET`    | `/categories`          | liste les catégories                             |
| `POST`   | `/categories`          | crée une catégorie : `{"name": "Meubles"}`       |
| `DELETE` | `/categories/{id}`     | supprime une catégorie (ses produits restent)    |

Corps de `POST /products` et `PUT /products/{id}` :

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
stock insuffisant), `401` clé d'API absente ou invalide, `422` données
invalides, `503` base indisponible.

Exemple :

```sh
K='X-API-Key: change-me-dev-key'
curl -H "$K" -X POST localhost:8080/categories -d '{"name":"Meubles"}'
curl -H "$K" -X POST localhost:8080/products \
  -d '{"reference":"C1","name":"Chaise","price_cents":4999,"stock":5,"category_id":1}'
curl -H "$K" -X POST localhost:8080/products/1/stock -d '{"delta":-2}'
curl -H "$K" 'localhost:8080/products?category_id=1'
```

## Docker

L'image est construite en deux étapes : compilation avec `golang:1.25-alpine`,
puis copie du seul binaire statique dans `distroless/static` (≈ 21 Mo, sans
shell, exécuté en utilisateur non-root). Le `HEALTHCHECK` de l'image appelle
`/claude healthcheck`, qui interroge `/healthz`.

```sh
make docker-build      # construit l'image claude:latest
make up                # lance PostgreSQL + l'API (lit API_KEYS dans .env)
make down              # arrête tout
```

Ou directement :

```sh
docker run -p 8080:8080 \
  -e DATABASE_URL='postgres://user:pass@host:5432/db?sslmode=require' \
  -e API_KEYS='une-clé-secrète' \
  claude:latest
```

## Tests

- `make test` : tests unitaires, sans base de données.
- `make test-integration` : tests unitaires et d'intégration contre la base de
  `DATABASE_URL`.
- `make smoke-test` : construit l'image Docker, la lance avec un PostgreSQL en
  conteneur et vérifie le comportement réel par HTTP (authentification,
  création, stock, healthcheck, utilisateur non-root, arrêt propre), puis
  supprime les conteneurs. `SKIP_BUILD=1` réutilise une image existante.

La CI GitHub Actions (`.github/workflows/ci.yml`) s'exécute à chaque push et
pull request : formatage, `go vet`, tous les tests avec un PostgreSQL de
service (avec le détecteur de concurrence `-race`), puis le smoke test Docker.

## Commandes

```sh
make build             # compile dans bin/claude
make run               # lance l'API
make test              # tests unitaires
make test-integration  # tests avec une vraie base (DATABASE_URL)
make fmt               # formate le code
make vet               # analyse statique
make db-up / db-down   # démarre / arrête PostgreSQL avec Docker
make docker-build      # construit l'image Docker
make smoke-test        # teste l'image dans de vrais conteneurs
make up / down         # démarre / arrête PostgreSQL + l'API en conteneurs
```
