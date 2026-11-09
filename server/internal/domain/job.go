package domain

import "time"

type JobType string

const (
	JobAnalyzeAudio   JobType = "analyze_audio"
	JobEnrichTrack    JobType = "enrich_track"
	JobEnrichAlbum    JobType = "enrich_album"
	JobFetchTrends    JobType = "fetch_trends"
	JobScrapeReleases JobType = "scrape_releases"
	JobDecayGraph     JobType = "decay_graph"
	JobBackupDB       JobType = "backup_db"
	JobSendScrobbles  JobType = "send_scrobbles"
)

type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
	JobDead    JobStatus = "dead"
)

type Job struct {
	ID          ID
	Type        JobType
	Payload     []byte
	Status      JobStatus
	Priority    int
	Attempts    int
	MaxAttempts int
	RunAfter    time.Time
	DedupKey    string
	LastError   string
}

// NextBackoff возвращает задержку повторной попытки, ограниченную шестью часами.
func (j *Job) NextBackoff() time.Duration {
	const maxDelay = 6 * time.Hour

	delay := time.Minute
	for i := 0; i < j.Attempts && delay < maxDelay; i++ {
		delay *= 4
	}
	return min(delay, maxDelay)
}

// Fail переводит job в failed/dead и назначает время ретрая
func (j *Job) Fail(errMsg string, now time.Time) {
	j.Attempts++
	j.LastError = errMsg
	if j.Attempts >= j.MaxAttempts {
		j.Status = JobDead
		return
	}
	j.Status = JobFailed
	j.RunAfter = now.Add(j.NextBackoff())
}
