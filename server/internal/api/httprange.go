package api

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/streaming"
)

var errBadRange = errors.New("bad range")

// httpRange — разобранный заголовок. length == -1 - до конца
type httpRange struct {
	offset, length int64
	isRange        bool
}

// parseRange — диапазон: bytes=X-", "bytes=X-Y", "bytes=-N"
func parseRange(h string, size int64) (httpRange, error) {
	if h == "" {
		return httpRange{0, -1, false}, nil
	}
	spec, ok := strings.CutPrefix(h, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return httpRange{}, errBadRange
	}
	start, end, found := strings.Cut(spec, "-")
	if !found {
		return httpRange{}, errBadRange
	}
	switch {
	case start == "": // bytes=-N: последние N байт
		n, err := strconv.ParseInt(end, 10, 64)
		if err != nil || n <= 0 {
			return httpRange{}, errBadRange
		}
		if n > size {
			n = size
		}
		return httpRange{size - n, n, true}, nil
	case end == "": // bytes=X-
		o, err := strconv.ParseInt(start, 10, 64)
		if err != nil || o < 0 || o >= size {
			return httpRange{}, errBadRange
		}
		return httpRange{o, -1, true}, nil
	default: // bytes=X-Y
		o, e1 := strconv.ParseInt(start, 10, 64)
		e, e2 := strconv.ParseInt(end, 10, 64)
		if e1 != nil || e2 != nil || o < 0 || o > e || o >= size {
			return httpRange{}, errBadRange
		}
		if e >= size {
			e = size - 1
		}
		return httpRange{o, e - o + 1, true}, nil
	}
}

// matchesIfNoneMatch сравнивает текущий сильный ETag с If-None-Match по слабому правилу.
func matchesIfNoneMatch(value, etag string) bool {
	if strings.Trim(value, " \t") == "*" {
		return true
	}

	for {
		value = strings.TrimLeft(value, " \t,")
		value = strings.TrimPrefix(value, "W/")
		if len(value) == 0 || value[0] != '"' {
			return false
		}

		end := strings.IndexByte(value[1:], '"')
		if end < 0 {
			return false
		}

		tag := value[:end+2]
		value = strings.TrimLeft(value[end+2:], " \t")
		if value != "" && value[0] != ',' {
			return false
		}
		if tag == etag {
			return true
		}
	}
}

// serveContent - отвечает на GET/HEAD с поддержкой Range и ETag
func (h *StreamHandler) serveContent(
	w http.ResponseWriter,
	r *http.Request,
	c *streaming.Content,
	asAttachment bool,
) {
	size := c.Blob.SizePlain
	etag := `"` + c.Blob.SHA256Plain + `"`
	notModified := matchesIfNoneMatch(
		strings.Join(r.Header.Values("If-None-Match"), ","),
		etag,
	)

	rangeHeader := ""
	if r.Method == http.MethodGet && !notModified {
		ifRange := strings.Trim(r.Header.Get("If-Range"), " \t")
		if ifRange == "" || ifRange == etag {
			rangeHeader = r.Header.Get("Range")
		}
	}

	rng, err := parseRange(rangeHeader, size)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		respond.JSON(w, http.StatusRequestedRangeNotSatisfiable, respond.ErrorBody{
			Error: "invalid range",
		})
		return
	}

	respLen := rng.length
	if respLen < 0 {
		respLen = size - rng.offset
	}

	// GET проверяет доступность файла до отправки заголовков. HEAD использует метаданные.
	var rc io.ReadCloser
	if r.Method != http.MethodHead {
		rc, _, err = h.stream.OpenRange(c, rng.offset, rng.length)
		if err != nil {
			respond.WriteError(w, h.log, err)
			return
		}
		defer rc.Close()
	}

	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)

	if notModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", c.Blob.MimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(respLen, 10))

	if asAttachment && c.Filename != "" {
		w.Header().Set(
			"Content-Disposition",
			"attachment; filename*=UTF-8''"+urlEncode(c.Filename),
		)
	}

	status := http.StatusOK
	if rng.isRange {
		w.Header().Set(
			"Content-Range",
			fmt.Sprintf(
				"bytes %d-%d/%d",
				rng.offset,
				rng.offset+respLen-1,
				size,
			),
		)
		status = http.StatusPartialContent
	}

	w.WriteHeader(status)

	if r.Method == http.MethodHead {
		return
	}

	written, err := io.Copy(w, rc)
	if err == nil && written != respLen {
		err = io.ErrUnexpectedEOF
	}
	if err == nil {
		return
	}

	if isClientDisconnect(err) {
		h.log.Debug("stream: client disconnected", "err", err)
	} else {
		h.log.Error(
			"stream: copy failed",
			"blob", c.Blob.ID,
			"written", written,
			"expected", respLen,
			"err", err,
		)
	}

	// Заголовки уже отправлены, поэтому прерываем передачу тела.
	panic(http.ErrAbortHandler)
}

func isClientDisconnect(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) &&
		opErr.Op == "write" &&
		isConnectionResetError(opErr.Err)
}

// urlEncode — percent-encoding для RFC 5987 (filename*).
func urlEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xF])
		}
	}
	return b.String()
}
