package api

// Handlers объединяет обработчики, которые регистрирует Routes.
type Handlers struct {
	Ingest  *IngestHandler
	Library *LibraryHandler
	Stream  *StreamHandler
	User    *UserHandler
}

// NewHandlers собирает обработчики для регистрации маршрутов.
func NewHandlers(
	ingest *IngestHandler,
	library *LibraryHandler,
	stream *StreamHandler,
	user *UserHandler,
) *Handlers {
	return &Handlers{
		Ingest:  ingest,
		Library: library,
		Stream:  stream,
		User:    user,
	}
}
