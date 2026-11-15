package jobs

import (
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const maxAttempts = 5

// Completed jobs are left out so a re-upload or regenerate can enqueue the same meeting again.
var uniqueOpts = river.UniqueOpts{
	ByArgs: true,
	ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
		rivertype.JobStateRunning, rivertype.JobStateScheduled,
	},
}

func insertOpts(queue string) river.InsertOpts {
	return river.InsertOpts{Queue: queue, MaxAttempts: maxAttempts, UniqueOpts: uniqueOpts}
}

type meetingArgs interface {
	river.JobArgs
	meetingID() uuid.UUID
}

type TranscribeArgs struct {
	MeetingID uuid.UUID `json:"meeting_id" river:"unique"`
}

func (TranscribeArgs) Kind() string                 { return "transcribe" }
func (TranscribeArgs) InsertOpts() river.InsertOpts { return insertOpts("transcribe") }
func (a TranscribeArgs) meetingID() uuid.UUID       { return a.MeetingID }

type SummarizeArgs struct {
	MeetingID uuid.UUID `json:"meeting_id" river:"unique"`
}

func (SummarizeArgs) Kind() string                 { return "summarize" }
func (SummarizeArgs) InsertOpts() river.InsertOpts { return insertOpts("summarize") }
func (a SummarizeArgs) meetingID() uuid.UUID       { return a.MeetingID }

type IndexMeetingArgs struct {
	MeetingID uuid.UUID `json:"meeting_id" river:"unique"`
}

func (IndexMeetingArgs) Kind() string                 { return "index_meeting" }
func (IndexMeetingArgs) InsertOpts() river.InsertOpts { return insertOpts("index") }
func (a IndexMeetingArgs) meetingID() uuid.UUID       { return a.MeetingID }
