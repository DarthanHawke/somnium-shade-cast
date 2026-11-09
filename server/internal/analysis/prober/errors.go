package prober

import "errors"

var (
	// ErrNotAudio - ffprobe разобрал файл, но не нашёл аудиопоток.
	ErrNotAudio = errors.New("prober: no audio stream")

	// ErrInvalidFile - входные данные пусты или ffprobe не смог их разобрать.
	ErrInvalidFile = errors.New("prober: invalid file")

	// ErrUnavailable - анализ сорвался при подготовке источника,
	// запуске ffprobe или обработке его результата.
	ErrUnavailable = errors.New("prober: ffprobe unavailable")
)
