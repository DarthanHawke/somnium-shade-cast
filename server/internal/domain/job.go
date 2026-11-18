package domain

import "time"

// JobType определяет обработчик для записи в очереди jobs.
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
	JobCollectBlobs   JobType = "collect_blobs"
	JobUploadTrack    JobType = "upload_track"
	JobPruneFinished  JobType = "prune_finished_jobs"
)

// Job содержит данные задачи для постановки и выполнения воркером.
type Job struct {
	ID          ID
	Type        JobType
	Payload     string
	Priority    int
	Attempts    int
	MaxAttempts int
	RunAfter    UnixTimeMillis
	DedupKey    string
	Every       time.Duration
	LeaseToken  string
}

// RetryDelay возвращает задержку после текущей попытки, от минуты до шести часов.
func (j Job) RetryDelay() time.Duration {
	const maxDelay = 6 * time.Hour

	delay := time.Minute
	for attempt := 1; attempt < j.Attempts && delay < maxDelay; attempt++ {
		delay *= 4
	}
	return min(delay, maxDelay)
}
