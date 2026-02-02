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
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING id, created_at, updated_at, email;

-- name: DeleteAllUsers :exec
DELETE FROM users;

-- name: UpdateUser :one
UPDATE users
SET email = $2,
    hashed_password = $3,
    updated_at = NOW()
WHERE id = $1
RETURNING id, email, created_at, updated_at;

