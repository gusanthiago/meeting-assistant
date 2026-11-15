-- name: UpsertTranscript :exec
INSERT INTO transcripts (meeting_id, workspace_id, language, segments)
VALUES (@meeting_id, @workspace_id, @language, @segments)
ON CONFLICT (meeting_id) DO UPDATE
SET language = EXCLUDED.language, segments = EXCLUDED.segments, created_at = now();

-- name: GetTranscript :one
SELECT * FROM transcripts WHERE workspace_id = @workspace_id AND meeting_id = @meeting_id;

-- name: DeleteTranscript :exec
DELETE FROM transcripts WHERE meeting_id = @meeting_id;

-- name: UpsertSummary :exec
INSERT INTO summaries (meeting_id, workspace_id, overview, takeaways, decisions, questions, provider, model, input_tokens, output_tokens)
VALUES (@meeting_id, @workspace_id, @overview, @takeaways, @decisions, @questions, @provider, sqlc.narg('model'), sqlc.narg('input_tokens'), sqlc.narg('output_tokens'))
ON CONFLICT (meeting_id) DO UPDATE
SET overview = EXCLUDED.overview, takeaways = EXCLUDED.takeaways, decisions = EXCLUDED.decisions,
    questions = EXCLUDED.questions, provider = EXCLUDED.provider, model = EXCLUDED.model,
    input_tokens = EXCLUDED.input_tokens, output_tokens = EXCLUDED.output_tokens, created_at = now();

-- name: GetSummary :one
SELECT * FROM summaries WHERE meeting_id = @meeting_id;

-- name: DeleteSummary :exec
DELETE FROM summaries WHERE meeting_id = @meeting_id;

-- name: DeleteActionItems :exec
DELETE FROM action_items WHERE meeting_id = @meeting_id;

-- name: InsertActionItem :exec
INSERT INTO action_items (meeting_id, workspace_id, position, text, owner, due, done)
VALUES (@meeting_id, @workspace_id, @position, @text, sqlc.narg('owner'), sqlc.narg('due'), @done);

-- name: ListActionItems :many
SELECT * FROM action_items WHERE meeting_id = @meeting_id ORDER BY position;

-- name: SetActionItemDone :one
UPDATE action_items SET done = @done, updated_at = now()
WHERE workspace_id = @workspace_id AND id = @id
RETURNING *;
