// Package ingest управляет загрузкой, материализацией и удалением аудиотреков.
package ingest

import (
	"context"
	"io"
	"log/slog"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// Storage сохраняет и удаляет зашифрованные файлы блобов.
type Storage interface {
	// Remove удаляет файл по пути rel относительно корня хранилища.
	// Отсутствие файла не считается ошибкой.
	Remove(rel string) error

	// Write шифрует данные из src и сохраняет их в новом файле.
	// Возвращает относительный путь и параметры шифрования,
	// необходимые для регистрации блоба. Не закрывает src.
	Write(src io.Reader) (*storage.WriteResult, error)

	// Open открывает блоб для чтения и проверяет первый чанк.
	// При отсутствии файла возвращает domain.ErrBlobFileMissing.
	// Возвращённый поток должен закрыть вызывающий код.
	Open(blob *domain.Blob) (io.ReadCloser, error)
}

// Spool принимает загрузки в буфер в памяти с ограничением размера.
type Spool interface {
	// Receive читает src и возвращает буфер с данными, размером и SHA-256.
	// При превышении допустимого размера возвращает domain.ErrTooLarge,
	// при сбое чтения — domain.ErrIncomplete.
	// Пустой поток отклоняется ошибкой валидации поля file.
	Receive(ctx context.Context, src io.Reader) (*storage.SpoolBuffer, error)
}

// TransactionRepository объединяет операции репозиториев в транзакцию.
type TransactionRepository interface {
	// WithTransaction выполняет fn в собственной транзакции.
	// Операции репозиториев внутри fn должны получать переданный ей контекст.
	// Если ctx уже содержит транзакцию, возвращает ошибку, не вызывая fn.
	// При ошибке или панике выполняет попытку отката.
	// Ошибка fn сохраняется; ошибка отката может быть присоединена к ней.
	// Паника продолжает распространяться после попытки отката.
	// Возвращает nil только после успешной фиксации транзакции.
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// BlobRepository предоставляет операции с блобами,
// необходимые для загрузки и удаления треков.
// При наличии транзакции в ctx методы выполняются в её рамках.
type BlobRepository interface {
	// Insert сохраняет метаданные блоба и при успехе заполняет blob.ID.
	// При совпадении SHA-256 возвращает domain.ErrBlobAlreadyExist.
	Insert(ctx context.Context, blob *domain.Blob) error

	// IncRef увеличивает число ссылок на блоб на один.
	// Если блоб отсутствует, возвращает domain.ErrNotFound.
	IncRef(ctx context.Context, id domain.ID) error

	// DecRef уменьшает число ссылок на один и возвращает новый счётчик.
	// Возвращает domain.ErrNotFound для отсутствующего блоба
	// и domain.ErrAlreadyOrphan для нулевого счётчика.
	DecRef(ctx context.Context, id domain.ID) (newCount int, err error)

	// GetBySHA256 возвращает метаданные блоба по хешу исходного файла.
	// Если блоб отсутствует, возвращает domain.ErrNotFound.
	GetBySHA256(ctx context.Context, shaPlain string) (*domain.Blob, error)

	// DeleteIfOrphan удаляет запись блоба с нулевым числом ссылок
	// и возвращает путь к файлу.
	// Возвращает domain.ErrNotFound для отсутствующего блоба
	// и domain.ErrBlobInUse для используемого.
	// Сам файл удаляется вызывающим кодом после фиксации транзакции.
	DeleteIfOrphan(ctx context.Context, id domain.ID) (storagePath string, err error)
}

// TrackRepository предоставляет операции с треками,
// необходимые для загрузки, материализации и удаления.
// При наличии транзакции в ctx методы выполняются в её рамках.
type TrackRepository interface {
	// Insert сохраняет трек и при успехе заполняет track.ID.
	// При нарушении уникальности возвращает domain.ErrConflict.
	Insert(ctx context.Context, track *domain.Track) error

	// GetByPublicID возвращает трек по публичному идентификатору.
	// Если трек отсутствует, возвращает domain.ErrNotFound.
	GetByPublicID(ctx context.Context, publicID domain.PublicID) (*domain.Track, error)

	// GetByOwnerAndSHA256 возвращает один из загруженных треков
	// владельца ownerID, связанных с исходным файлом с SHA-256 sha.
	// При нескольких совпадениях выбор трека не определён.
	// Если совпадений нет, возвращает domain.ErrNotFound.
	GetByOwnerAndSHA256(ctx context.Context, sha string, ownerID domain.ID) (*domain.Track, error)

	// Materialize сохраняет данные загруженного файла в ghost-трек.
	// track должен содержать статус domain.TrackUploaded
	// и BlobID загруженного файла.
	// Название, исполнитель, альбом и нумерация остаются прежними.
	// Возвращает domain.ErrConflict, если не найдена запись
	// с указанными PublicID и OwnerID в статусе ghost без блоба.
	Materialize(ctx context.Context, track *domain.Track) error

	// Delete удаляет запись трека по публичному идентификатору.
	// Не удаляет файл блоба, не изменяет число ссылок и поисковый индекс.
	// Если трек отсутствует, возвращает domain.ErrNotFound.
	Delete(ctx context.Context, publicID domain.PublicID) error
}

// ArtistRepository предоставляет данные исполнителей, необходимые ingest.
// При наличии транзакции в ctx методы выполняются в её рамках.
type ArtistRepository interface {
	// GetByID возвращает исполнителя по внутреннему идентификатору.
	// Если исполнитель отсутствует, возвращает domain.ErrNotFound.
	GetByID(ctx context.Context, id domain.ID) (*domain.Artist, error)
}

// AlbumRepository предоставляет данные альбомов, необходимые ingest.
// При наличии транзакции в ctx методы выполняются в её рамках.
type AlbumRepository interface {
	// GetByID возвращает альбом по внутреннему идентификатору.
	// Если альбом отсутствует, возвращает domain.ErrNotFound.
	GetByID(ctx context.Context, id domain.ID) (*domain.Album, error)
}

// SearchRepository поддерживает поисковый индекс загружаемых треков.
// При наличии транзакции в ctx методы выполняются в её рамках.
type SearchRepository interface {
	// Insert добавляет в индекс название трека, имя исполнителя
	// и название альбома для trackID.
	Insert(ctx context.Context, trackID domain.ID, title, artistName, albumTitle string) error

	// Remove удаляет запись трека из индекса.
	// title, artistName и albumTitle должны соответствовать значениям,
	// использованным при индексации.
	Remove(ctx context.Context, trackID domain.ID, title, artistName, albumTitle string) error
}

// Metadata извлекает метаданные аудиофайлов
// и связывает их с исполнителями и альбомами библиотеки.
type Metadata interface {
	// Extract извлекает метаданные из src размером size байт.
	// originalFilename используется для получения названия трека,
	// если оно отсутствует в тегах.
	// Неподдерживаемый аудиофайл отклоняется ошибкой валидации поля file.
	Extract(
		ctx context.Context,
		src io.ReaderAt,
		size int64,
		originalFilename string,
	) (*domain.ExtractedMetadata, error)

	// Resolve находит или создаёт исполнителя и альбом по extracted.
	// При успехе исполнитель всегда задан.
	// Если название альбома отсутствует, возвращает nil вместо альбома.
	// Использует транзакцию из ctx, если она задана.
	Resolve(ctx context.Context, extracted *domain.ExtractedMetadata) (*domain.Artist, *domain.Album, error)

	// GetNamesByID возвращает имя исполнителя и название альбома.
	// При albumID == nil возвращает пустое название альбома.
	// Если указанный исполнитель или альбом отсутствует,
	// возвращает domain.ErrNotFound.
	// Использует транзакцию из ctx, если она задана.
	GetNamesByID(ctx context.Context, artistID domain.ID, albumID *domain.ID) (string, string, error)
}

// Jobs задаёт контракт постановки фоновых задач.
type Jobs interface {
	// Enqueue запрашивает постановку фоновой задачи с указанными параметрами.
	// Не ожидает выполнения задачи.
	Enqueue(ctx context.Context, jobType domain.JobType, payload any, opts ...jobs.Option) error
}

// Service координирует хранение аудиофайлов и изменение библиотеки треков.
type Service struct {
	store    Storage
	spool    Spool
	tx       TransactionRepository
	blobs    BlobRepository
	tracks   TrackRepository
	search   SearchRepository
	artists  ArtistRepository
	albums   AlbumRepository
	jobs     Jobs
	metadata Metadata
	log      *slog.Logger
}

// Dependencies содержит зависимости сервиса загрузки.
// Все поля обязательны.
type Dependencies struct {
	Store    Storage
	Spool    Spool
	Tx       TransactionRepository
	Blobs    BlobRepository
	Tracks   TrackRepository
	Search   SearchRepository
	Artists  ArtistRepository
	Albums   AlbumRepository
	Jobs     Jobs
	Metadata Metadata
	Logger   *slog.Logger
}

// New создаёт сервис с указанными зависимостями.
func New(deps Dependencies) *Service {
	return &Service{
		store:    deps.Store,
		spool:    deps.Spool,
		tx:       deps.Tx,
		blobs:    deps.Blobs,
		tracks:   deps.Tracks,
		search:   deps.Search,
		artists:  deps.Artists,
		albums:   deps.Albums,
		jobs:     deps.Jobs,
		metadata: deps.Metadata,
		log:      deps.Logger,
	}
}
