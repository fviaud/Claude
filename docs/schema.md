# Schéma de la base de données

Base PostgreSQL 16. Le schéma est créé par les migrations SQL de
[`internal/database/migrations/`](../internal/database/migrations/), appliquées
automatiquement au démarrage de l'application, dans l'ordre de leur nom.

| Migration | Contenu |
|---|---|
| [`0001_create_catalogue.sql`](../internal/database/migrations/0001_create_catalogue.sql) | table `catalogue` |
| [`0002_add_category_and_stock.sql`](../internal/database/migrations/0002_add_category_and_stock.sql) | table `categories`, colonnes `catalogue.category_id` et `catalogue.stock` |

## Diagramme

```mermaid
erDiagram
    categories |o--o{ catalogue : "regroupe"

    categories {
        BIGINT id PK "auto-incrémenté"
        TEXT name UK "obligatoire"
        TIMESTAMPTZ created_at "now()"
    }

    catalogue {
        BIGINT id PK "auto-incrémenté"
        TEXT reference UK "obligatoire"
        TEXT name "obligatoire, indexé"
        TEXT description "'' par défaut"
        BIGINT price_cents "≥ 0"
        BIGINT category_id FK "facultatif"
        INTEGER stock "≥ 0, 0 par défaut"
        TIMESTAMPTZ created_at "now()"
        TIMESTAMPTZ updated_at "now()"
    }

    schema_migrations {
        TEXT version PK "nom du fichier sans .sql"
        TIMESTAMPTZ applied_at "now()"
    }
```

Une catégorie regroupe zéro, un ou plusieurs articles ; un article appartient à
zéro ou une catégorie.

## Tables

### `catalogue`

Les articles du catalogue.

| Colonne | Type | Null | Défaut | Description |
|---|---|---|---|---|
| `id` | `BIGINT` | non | identité (auto) | identifiant de l'article |
| `reference` | `TEXT` | non | — | référence métier, unique |
| `name` | `TEXT` | non | — | nom de l'article |
| `description` | `TEXT` | non | `''` | description libre |
| `price_cents` | `BIGINT` | non | — | prix en centimes (4999 = 49,99) |
| `category_id` | `BIGINT` | **oui** | `NULL` | catégorie de l'article, → `categories.id` |
| `stock` | `INTEGER` | non | `0` | quantité disponible |
| `created_at` | `TIMESTAMPTZ` | non | `now()` | date de création |
| `updated_at` | `TIMESTAMPTZ` | non | `now()` | date de dernière modification |

| Contrainte / index | Type | Rôle |
|---|---|---|
| `catalogue_pkey` | clé primaire | sur `id` |
| `catalogue_reference_key` | unique | deux articles ne peuvent pas avoir la même référence |
| `catalogue_price_cents_check` | `CHECK (price_cents >= 0)` | pas de prix négatif |
| `catalogue_stock_check` | `CHECK (stock >= 0)` | pas de stock négatif |
| `catalogue_category_id_fkey` | clé étrangère → `categories(id)` `ON DELETE SET NULL` | voir ci-dessous |
| `catalogue_name_idx` | index | tri et recherche par nom |
| `catalogue_category_id_idx` | index | liste des articles d'une catégorie |

### `categories`

Les catégories d'articles.

| Colonne | Type | Null | Défaut | Description |
|---|---|---|---|---|
| `id` | `BIGINT` | non | identité (auto) | identifiant de la catégorie |
| `name` | `TEXT` | non | — | nom, unique |
| `created_at` | `TIMESTAMPTZ` | non | `now()` | date de création |

| Contrainte / index | Type | Rôle |
|---|---|---|
| `categories_pkey` | clé primaire | sur `id` |
| `categories_name_key` | unique | deux catégories ne peuvent pas avoir le même nom |

### `schema_migrations`

Table technique tenue par l'application : une ligne par migration appliquée.
Ne pas la modifier à la main.

| Colonne | Type | Null | Défaut | Description |
|---|---|---|---|---|
| `version` | `TEXT` | non | — | nom du fichier de migration sans `.sql`, clé primaire |
| `applied_at` | `TIMESTAMPTZ` | non | `now()` | date d'application |

## Règles métier portées par la base

Ces règles sont garanties par PostgreSQL lui-même, quelle que soit la façon
dont on écrit dans la base (API, `psql`, script…).

- **Référence unique** : créer ou renommer un article avec une référence déjà
  prise échoue. L'API répond `409`.
- **Nom de catégorie unique** : même comportement, `409`.
- **Prix et stock jamais négatifs** : toute écriture qui rendrait `price_cents`
  ou `stock` négatif est refusée. Le retrait de stock (`POST /items/{id}/stock`)
  est fait en une seule requête `UPDATE … SET stock = stock + delta`, donc deux
  retraits simultanés ne peuvent pas faire passer le stock sous zéro. L'API
  répond `409 insufficient stock`.
- **Catégorie existante** : un article ne peut pointer que vers une catégorie
  qui existe. L'API répond `422`.
- **Suppression d'une catégorie** : ses articles sont conservés et leur
  `category_id` passe à `NULL` (`ON DELETE SET NULL`).

Correspondance avec le code Go : ces erreurs PostgreSQL sont converties en
`ErrDuplicateReference`, `ErrDuplicateCategory`, `ErrInsufficientStock` et
`ErrCategoryNotFound` dans [`internal/catalogue`](../internal/catalogue/), à
partir du nom de la contrainte en cause.

## Faire évoluer le schéma

1. Ajouter un fichier numéroté à la suite dans `internal/database/migrations/`,
   par exemple `0003_add_image_url.sql`.
2. **Ne jamais modifier un fichier déjà appliqué** : la base ne le rejouerait
   pas, et les environnements divergeraient.
3. Chaque fichier est exécuté dans une transaction : en cas d'erreur, rien
   n'est appliqué et l'application ne démarre pas.
4. Mettre à jour ce document. Si une contrainte est renommée ou ajoutée,
   vérifier la conversion des erreurs dans `internal/catalogue/catalogue.go`.

## Consulter le schéma d'une base

```sh
psql "$DATABASE_URL" -c '\d catalogue' -c '\d categories'
psql "$DATABASE_URL" -c 'SELECT * FROM schema_migrations ORDER BY version'
```
