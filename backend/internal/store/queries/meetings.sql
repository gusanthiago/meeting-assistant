-- name: InsertMeeting :one
INSERT INTO meetings (workspace_id, title, participants, status, idempotency_key, started_at)
VALUES (@workspace_id, @title, @participants, @status, sqlc.narg('idempotency_key'), sqlc.narg('started_at'))
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetMeeting :one
SELECT * FROM meetings WHERE workspace_id = @workspace_id AND id = @id;

-- name: GetMeetingByID :one
SELECT * FROM meetings WHERE id = @id;

-- name: LockMeeting :one
SELECT * FROM meetings WHERE workspace_id = @workspace_id AND id = @id FOR UPDATE;

-- name: GetMeetingByIdempotencyKey :one
SELECT * FROM meetings WHERE workspace_id = @workspace_id AND idempotency_key = @idempotency_key;

-- name: ListMeetings :many
SELECT * FROM meetings
WHERE workspace_id = @workspace_id
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT @lim;

-- name: SetMeetingStatus :one
UPDATE meetings SET status = @status, error = sqlc.narg('error'), summary_error = sqlc.narg('summary_error'), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SetMeetingRecording :one
UPDATE meetings
SET audio_key = @audio_key,
    audio_content_type = @audio_content_type,
    audio_size = @audio_size,
    duration_ms = COALESCE(sqlc.narg('duration_ms'), duration_ms),
    status = @status,
    error = NULL,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ListReadyMeetingIDs :many
SELECT id FROM meetings WHERE status = 'ready' ORDER BY id;

-- name: SetMeetingDurationIfMissing :exec
UPDATE meetings SET duration_ms = @duration_ms WHERE id = @id AND duration_ms IS NULL;

-- name: LockMeetingByID :one
SELECT * FROM meetings WHERE id = @id FOR UPDATE;

-- name: DeleteMeeting :execrows
DELETE FROM meetings WHERE workspace_id = @workspace_id AND id = @id;
