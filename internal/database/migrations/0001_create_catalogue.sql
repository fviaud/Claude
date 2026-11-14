CREATE TABLE catalogue (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reference   TEXT        NOT NULL UNIQUE,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    price_cents BIGINT      NOT NULL CHECK (price_cents >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX catalogue_name_idx ON catalogue (name);
