#!/usr/bin/env bash
# End-to-end checks against the running stack (`make up` first).
set -euo pipefail

API=${API:-http://localhost:8080}
TMP=$(mktemp -d)
ID=""
# Removes the test meeting even when a check fails, so runs don't pile up in the meeting list.
cleanup() {
  if [[ -n $ID ]]; then curl -s -o /dev/null -X DELETE "$API/v1/meetings/$ID" || true; fi
  rm -rf "$TMP"
}
trap cleanup EXIT

pass() { printf '  \033[32mok\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; exit 1; }
# Evaluates a Python expression on the JSON body; API envelopes are unwrapped to their data.
json() { python3 -c "import sys,json; d=json.load(sys.stdin); d=d['data'] if isinstance(d,dict) and 'code' in d and 'data' in d else d; print(eval(sys.argv[1]))" "$1"; }

wait_for() { # wait_for <seconds> <description> <command...>
  local deadline=$((SECONDS + $1)) what=$2; shift 2
  until "$@" >/dev/null 2>&1; do
    ((SECONDS < deadline)) || fail "timed out: $what"
    sleep 2
  done
}

status_of() { curl -sf "$API/v1/meetings/$1" | json "d['status']"; }
is_final() { s=$(status_of "$1"); [[ $s == ready || $s == failed ]]; }

echo "Meeting Assistant smoke test"
wait_for 180 "API ready" curl -sf "$API/readyz"
pass "API, Postgres, OpenSearch and storage are ready"

SPEECH="Thanks for joining. Bruno will send the release notes by Friday. Do we need a migration window?"
if [[ -n ${SAMPLE_AUDIO:-} ]]; then
  cp "$SAMPLE_AUDIO" "$TMP/speech.aiff"; CT=${SAMPLE_TYPE:-audio/wav}
elif command -v say >/dev/null; then
  say -o "$TMP/speech.aiff" "$SPEECH"; CT=audio/aiff
else
  fail "needs macOS 'say', or SAMPLE_AUDIO=<file with speech> SAMPLE_TYPE=<audio/...>"
fi

TITLE="Smoke test $RANDOM$RANDOM"
KEY=$(python3 -c "import uuid; print(uuid.uuid4())")
BODY="{\"title\":\"$TITLE\",\"participants\":[\"Ana\",\"Bruno\"]}"
ID=$(curl -sf -X POST "$API/v1/meetings" -H "Content-Type: application/json" -H "Idempotency-Key: $KEY" -d "$BODY" | json "d['id']")
AGAIN=$(curl -sf -o "$TMP/again.json" -w '%{http_code}' -X POST "$API/v1/meetings" -H "Content-Type: application/json" -H "Idempotency-Key: $KEY" -d "$BODY")
[[ $AGAIN == 200 && $(json "d['id']" <"$TMP/again.json") == "$ID" ]] || fail "Idempotency-Key replay returned a new meeting"
pass "create meeting $ID (Idempotency-Key replay returns the same one)"

CODE=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$API/v1/meetings/$ID/recording" -H "Content-Type: $CT" --data-binary @"$TMP/speech.aiff")
[[ $CODE == 202 ]] || fail "upload returned $CODE"
CODE=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$API/v1/meetings/$ID/recording" -H "Content-Type: text/plain" --data-binary "nope")
[[ $CODE == 415 || $CODE == 409 ]] || fail "non-audio upload returned $CODE"
pass "upload recording (202), reject non-audio ($CODE)"

wait_for 300 "meeting ready (the first run downloads the Whisper model)" is_final "$ID"
[[ $(status_of "$ID") == ready ]] || fail "meeting failed: $(curl -s "$API/v1/meetings/$ID" | json "d.get('error')")"
SEGMENTS=$(curl -sf "$API/v1/meetings/$ID/transcript" | json "len(d['segments'])")
ITEMS=$(curl -sf "$API/v1/meetings/$ID" | json "len(d['action_items'])")
OVERVIEW=$(curl -sf "$API/v1/meetings/$ID" | json "bool(d['summary']['overview'])")
[[ $SEGMENTS -gt 0 && $OVERVIEW == True ]] || fail "missing transcript or summary"
pass "pipeline: $SEGMENTS transcript segments, summary, $ITEMS action items"
TEXT=$(curl -sf "$API/v1/meetings/$ID/transcript" | json "' '.join(s['text'] for s in d['segments'])")
[[ $TEXT == *"release notes"* ]] || fail "the transcript doesn't contain the spoken words: $TEXT"
pass "Whisper heard: \"$TEXT\""

ETAG=$(curl -sfI "$API/v1/meetings/$ID" | tr -d '\r' | awk -F': ' 'tolower($1)=="etag"{print $2}')
CODE=$(curl -s -o /dev/null -w '%{http_code}' -H "If-None-Match: $ETAG" "$API/v1/meetings/$ID")
[[ $CODE == 304 ]] || fail "If-None-Match returned $CODE"
pass "polling with ETag returns 304"



WORD=${TITLE##* }
found() { curl -sf "$API/v1/search?q=$1" | json "'$ID' in [h['meeting_id'] for h in d['items']]" | grep -q True; }
wait_for 60 "search finds the meeting" found "$WORD"
found Ana || fail "search doesn't find the meeting by participant"
pass "search finds the meeting by title and by participant"

CODE=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$API/v1/meetings/$ID")
AFTER=$(curl -s -o /dev/null -w '%{http_code}' "$API/v1/meetings/$ID")
[[ $CODE == 204 && $AFTER == 404 ]] || fail "delete returned $CODE, then the meeting returned $AFTER"
ID=""
pass "delete removes the meeting"

echo "All checks passed."
