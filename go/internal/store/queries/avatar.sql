-- name: AvatarPreference :one
SELECT consent, appearance FROM avatar_preferences WHERE user_id=$1 AND company=$2;

-- name: SetAvatarPreference :exec
INSERT INTO avatar_preferences (user_id,company,consent,appearance) VALUES ($1,$2,$3,$4)
ON CONFLICT (user_id,company) DO UPDATE SET consent=EXCLUDED.consent, appearance=EXCLUDED.appearance;

-- name: RemoveAvatarPreference :exec
DELETE FROM avatar_preferences WHERE user_id=$1 AND company=$2;
