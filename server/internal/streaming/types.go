package streaming

import "github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"

// Content содержит метаданные блоба и имя файла для скачивания.
type Content struct {
	Blob     *domain.Blob // Blob содержит метаданные файла в хранилище.
	Filename string       // Filename задаёт имя аудиофайла для скачивания. Для обложки поле пустое.
}
