package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// Spool принимает загрузки в буфер в памяти с ограничением размера.
type Spool struct {
	maxSize int64 // maxSize лимит одного файла.
}

// NewSpool создаёт приёмник с лимитом maxSize байт на одну загрузку.
// maxSize должен быть положительным и меньше math.MaxInt64.
func NewSpool(maxSize int64) (*Spool, error) {
	if maxSize <= 0 || maxSize == math.MaxInt64 {
		return nil, fmt.Errorf("spool: invalid maximum file size: %d", maxSize)
	}
	return &Spool{maxSize: maxSize}, nil
}

// SpoolBuffer содержит загруженные данные, их размер и SHA-256.
// Данные доступны для чтения через независимые курсоры и ReadAt.
type SpoolBuffer struct {
	data   []byte
	sha256 string
}

// Receive читает src в память и вычисляет SHA-256 исходных данных.
// Возвращает domain.ErrTooLarge при превышении лимита
// и ошибку валидации поля file для пустого потока.
// При ошибке чтения возвращает domain.ErrIncomplete, сохраняя исходную причину.
// Проверяет отмену ctx между чтениями и перед возвратом результата.
// Не закрывает src.
func (s *Spool) Receive(ctx context.Context, src io.Reader) (*SpoolBuffer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hr := domain.NewHashReader(io.LimitReader(
		contextReader{ctx: ctx, src: src},
		s.maxSize+1, // Дополнительный байт позволяет обнаружить превышение лимита.
	))
	data, err := io.ReadAll(hr)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("%w: spool: receive: %w", domain.ErrIncomplete, ctxErr)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: spool: receive: %w", domain.ErrIncomplete, err)
	}
	if int64(len(data)) > s.maxSize {
		return nil, domain.ErrTooLarge
	}
	if len(data) == 0 {
		return nil, domain.Invalid("file", "empty upload")
	}

	return &SpoolBuffer{data: data, sha256: hr.SumHex()}, nil
}

// Size возвращает размер загруженных данных в байтах.
func (b *SpoolBuffer) Size() int64 {
	return int64(len(b.data))
}

// SHA256 возвращает SHA-256 загруженных данных в шестнадцатеричном представлении.
func (b *SpoolBuffer) SHA256() string {
	return b.sha256
}

// Reader возвращает новый курсор чтения загруженных данных.
// Изменение его позиции не влияет на другие курсоры; исходные данные не копируются.
func (b *SpoolBuffer) Reader() *bytes.Reader {
	return bytes.NewReader(b.data)
}

// ReadAt читает данные с байтового смещения off, не изменяя позиции курсоров Reader.
// Допускает одновременные вызовы из нескольких горутин.
func (b *SpoolBuffer) ReadAt(p []byte, off int64) (int, error) {
	return bytes.NewReader(b.data).ReadAt(p, off)
}

// contextReader проверяет отмену перед очередным чтением src.
type contextReader struct {
	ctx context.Context
	src io.Reader
}

// Read проверяет отмену ctx перед чтением из src.
// Вызовы должны быть последовательными,
// иначе может быть возвращён context.Canceled до чтения.
func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.src.Read(p)
}
