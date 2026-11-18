// Package ingest загружает аудиофайлы и создаёт или материализует треки.
package ingest

import (
	"context"
	"io"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// Storage сохраняет и удаляет зашифрованные файлы блобов.
type Storage interface {
	// Write шифрует данные из src и сохраняет их в новом файле.
	// Возвращает относительный путь и параметры шифрования,
	// необходимые для регистрации блоба. Не закрывает src.
	Write(src io.Reader) (*storage.WriteResult, error)

	// Open открывает блоб для чтения и проверяет первый чанк.
	// При отсутствии файла возвращает domain.ErrBlobFileMissing.
	// Возвращённый поток должен закрыть вызывающий код.
	Open(blob *domain.Blob) (io.ReadCloser, error)

	// OpenReaderAt открывает расшифрованное содержимое для анализа по смещениям.
	OpenReaderAt(blob *domain.Blob) (storage.ReadAtCloser, error)

	// ProtectUpload защищает выбор или запись файла от GC до завершения регистрации.
	// Возвращённую функцию нужно вызвать после завершения транзакции.
	ProtectUpload() func()
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
// необходимые для загрузки треков.
// При наличии транзакции в ctx методы выполняются в её рамках.
type BlobRepository interface {
	// Insert сохраняет метаданные блоба и при успехе заполняет blob.ID.
	// При совпадении SHA-256 возвращает domain.ErrBlobAlreadyExist.
	Insert(ctx context.Context, blob *domain.Blob) error

	// IncRef увеличивает число ссылок на блоб на один.
	// Если блоб отсутствует, возвращает domain.ErrNotFound.
	IncRef(ctx context.Context, id domain.ID) error

	// GetBySHA256 возвращает метаданные блоба по хешу исходного файла.
	// Если блоб отсутствует, возвращает domain.ErrNotFound.
	GetBySHA256(ctx context.Context, shaPlain string) (*domain.Blob, error)

	// GetByID возвращает параметры расшифровки принятого блоба.
	GetByID(ctx context.Context, id domain.ID) (*domain.Blob, error)

	// SetAudioMIME сохраняет MIME-тип, определённый при анализе аудио.
	SetAudioMIME(ctx context.Context, id domain.ID, mime string) error
}

// TrackRepository предоставляет операции с треками,
// необходимые для загрузки, материализации.
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
}

// AlbumRepository привязывает обложку к альбому.
type AlbumRepository interface {
	// SetCoverIfAbsent привязывает image-блоб к альбому без обложки.
	// Использует транзакцию из ctx, если та задана.
	SetCoverIfAbsent(ctx context.Context, albumID, blobID domain.ID) error
}

// SearchRepository поддерживает поисковый индекс загружаемых треков.
// При наличии транзакции в ctx методы выполняются в её рамках.
type SearchRepository interface {
	// IndexTrack индексирует актуальные метаданные загруженного трека.
	// Если загруженный трек отсутствует, возвращает domain.ErrNotFound.
	IndexTrack(ctx context.Context, trackID domain.ID) error
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
}

// UploadRepository хранит принятые файлы и результаты обработки.
type UploadRepository interface {
	// ByKey возвращает ранее принятую загрузку того же пользователя.
	// Если совпадений нет, возвращает domain.ErrNotFound.
	ByKey(ctx context.Context, owner domain.ID, key string) (*domain.TrackUpload, error)

	// ForOwner возвращает загрузку только его владельцу.
	// Если совпадений нет, возвращает domain.ErrNotFound.
	ForOwner(ctx context.Context, owner domain.ID, id domain.PublicID) (*domain.TrackUpload, error)

	// ByPublicID возвращает загрузку для фонового обработчика.
	// Если совпадений нет, возвращает domain.ErrNotFound.
	ByPublicID(ctx context.Context, id domain.PublicID) (*domain.TrackUpload, error)

	// ListForOwner возвращает страницу истории загрузок от новых к старым.
	ListForOwner(ctx context.Context, owner domain.ID, limit, offset int) ([]domain.TrackUpload, error)

	// Insert регистрирует принятый файл. Вызывается в общей транзакции с постановкой задачи.
	Insert(ctx context.Context, item *domain.TrackUpload) error

	// LockAttempt проверяет право текущей попытки завершить загрузку. UPDATE получает
	// блокировку записи до работы с треком, чтобы аренда не могла смениться до commit.
	LockAttempt(ctx context.Context, id domain.PublicID, job domain.Job) (bool, error)

	// Finish завершает загрузку, сохраняя результат или код ошибки.
	Finish(ctx context.Context, id domain.PublicID, track *domain.PublicID, errorCode *string) error
}

// UploadQueue ставит задачу через транзакцию приёма файла.
type UploadQueue interface {
	// Enqueue возвращает внутренний ID созданной задачи.
	Enqueue(ctx context.Context, kind domain.JobType, payload any, opts ...jobs.Option) (domain.ID, error)
}

// Service принимает файлы и управляет фоновой обработкой загрузок.
type Service struct {
	store    Storage
	spool    Spool
	tx       TransactionRepository
	blobs    BlobRepository
	tracks   TrackRepository
	album    AlbumRepository
	search   SearchRepository
	metadata Metadata
	uploads  UploadRepository
	queue    UploadQueue
}

// Dependencies содержит зависимости сервиса загрузки.
// Все поля обязательны.
type Dependencies struct {
	Store    Storage
	Spool    Spool
	Tx       TransactionRepository
	Blobs    BlobRepository
	Tracks   TrackRepository
	Album    AlbumRepository
	Search   SearchRepository
	Metadata Metadata
	Uploads  UploadRepository
	Queue    UploadQueue
}

// New создаёт сервис с указанными зависимостями.
func New(deps Dependencies) *Service {
	return &Service{
		store:    deps.Store,
		spool:    deps.Spool,
		tx:       deps.Tx,
		blobs:    deps.Blobs,
		tracks:   deps.Tracks,
		album:    deps.Album,
		search:   deps.Search,
		metadata: deps.Metadata,
		uploads:  deps.Uploads,
		queue:    deps.Queue,
	}
}
