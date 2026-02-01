-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1
)
RETURNING *;

-- name: GetUserByEmail :one
SELECT id, email, created_at, updated_at, hashed_password
FROM users
WHERE email = $1;

-- name: CreateUserWithPassword :one
INSERT INTO users (email, hashed_password)
VALUES  ($1, $2)
RETURNING id, email, created_at, updated_at;

-- name: DeleteAllUsers :exec
DELETE FROM users;
