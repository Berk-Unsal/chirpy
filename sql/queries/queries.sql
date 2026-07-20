-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING *;

-- name: UpdateUser :exec
UPDATE users
SET
    hashed_password = $1,
    email = $2
WHERE
    id = $3;

-- name: WipeUsers :exec
DELETE FROM users;

-- name: CreateChirp :one
INSERT INTO chirps (id, created_at, updated_at, body, user_id)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING *;

-- name: GetAllChirps :many
SELECT id, created_at, updated_at, body, user_id
FROM chirps
ORDER BY created_at ASC;


-- name: GetUserPassword :one
SELECT hashed_password FROM users WHERE email = $1;

-- name: GetUser :one
SELECT * FROM users WHERE email = $1;

-- name: GetRefreshToken :one
SELECT * FROM refresh_tokens WHERE user_id = $1;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (token, created_at, updated_at, user_id, expires_at, revoked_at)
VALUES (
    $1,
    NOW(),
    NOW(),
    $2,
    $3,
    NULL
)
RETURNING *;

-- name: GetUserFromRefreshToken :one
SELECT user_id FROM refresh_tokens WHERE token = $1 AND revoked_at IS NULL AND expires_at > NOW();


-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET
    updated_at = NOW(),
    revoked_at = NOW()
WHERE
    token = $1;

-- name: DeleteChirp :exec
DELETE FROM chirps WHERE id = $1;

-- name: GetChirp :one
SELECT * FROM chirps WHERE id = $1;

-- name: UpgradeUserToRed :one
UPDATE users SET is_chirpy_red = true WHERE id = $1 RETURNING *;

-- name: GetChirpsFromUser :many
SELECT * FROM chirps WHERE user_id = $1;
