-- +goose Up
CREATE TABLE avatar_preferences (
    user_id text NOT NULL,
    company text NOT NULL,
    consent boolean NOT NULL DEFAULT false,
    appearance text NOT NULL CHECK (appearance IN ('human-blue','human-coral','human-green')),
    PRIMARY KEY (user_id, company)
);

-- +goose Down
DROP TABLE avatar_preferences;
