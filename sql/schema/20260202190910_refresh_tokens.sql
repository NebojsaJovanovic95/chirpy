-- +goose Up
-- +goose StatementBegin
CREATE TABLE refresh_tokens (
    token TEXT PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL
);
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
-- Rollback
DROP TABLE refresh_tokens;
-- +goose StatementEnd
