CREATE TABLE categories (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE catalogue
    ADD COLUMN category_id BIGINT REFERENCES categories (id) ON DELETE SET NULL,
    ADD COLUMN stock       INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0);

CREATE INDEX catalogue_category_id_idx ON catalogue (category_id);
