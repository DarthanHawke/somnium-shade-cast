package tag

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestFLACPictureBounds(t *testing.T) {
	cover := []byte("image bytes")
	for _, tc := range []struct {
		name  string
		block []byte
		want  []byte
	}{
		{"valid", pictureFixture(uint32(len(cover)), cover), cover},
		{"short data", pictureFixture(4<<20, nil), nil},
		{"short description", shortDescriptionFixture(), nil},
		{"too large", make([]byte, maxPictureBlockBytes+1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := []byte("fLaC")
			file = appendFLACBlock(file, 0, make([]byte, 34))
			file = appendFLACBlock(file, 6, tc.block)
			file = appendFLACBlock(file, 0x84, vorbisFixture("TITLE=Kept title"))

			meta, err := ReadFrom(bytes.NewReader(file))
			if err != nil {
				t.Fatal(err)
			}
			if meta.Title() != "Kept title" {
				t.Fatalf("title = %q", meta.Title())
			}
			assertPicture(t, meta.Picture(), tc.want)
		})
	}
}

func TestFLACRejectsTruncatedBlock(t *testing.T) {
	file := []byte("fLaC")
	file = appendFLACBlock(file, 0, make([]byte, 34))
	file = append(file, 0x86, 0, 0, 100)
	if _, err := ReadFrom(bytes.NewReader(file)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected truncated block error, got %v", err)
	}
}

func TestVorbisPictureBounds(t *testing.T) {
	cover := []byte("image bytes")
	for _, tc := range []struct {
		name    string
		encoded string
		want    []byte
	}{
		{"valid", base64.StdEncoding.EncodeToString(pictureFixture(uint32(len(cover)), cover)), cover},
		{"bad base64", "invalid!", nil},
		{"short data", base64.StdEncoding.EncodeToString(pictureFixture(4<<20, nil)), nil},
		{"too large", base64.StdEncoding.EncodeToString(make([]byte, maxPictureBlockBytes+1)), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMetadataVorbis()
			packet := vorbisFixture("TITLE=Kept title", "METADATA_BLOCK_PICTURE="+tc.encoded)
			if err := m.readVorbisComment(bytes.NewReader(packet)); err != nil {
				t.Fatal(err)
			}
			if m.Title() != "Kept title" {
				t.Fatalf("title = %q", m.Title())
			}
			assertPicture(t, m.Picture(), tc.want)
		})
	}
}

func assertPicture(t *testing.T, picture *Picture, want []byte) {
	t.Helper()
	if want == nil {
		if picture != nil {
			t.Fatal("unexpected picture")
		}
		return
	}
	if picture == nil || picture.MIMEType != "image/png" || !bytes.Equal(picture.Data, want) {
		t.Fatalf("unexpected picture: %+v", picture)
	}
}

func pictureFixture(length uint32, data []byte) []byte {
	block := binary.BigEndian.AppendUint32(nil, 3)
	block = binary.BigEndian.AppendUint32(block, uint32(len("image/png")))
	block = append(block, "image/png"...)
	block = binary.BigEndian.AppendUint32(block, 0)
	block = append(block, make([]byte, 16)...)
	block = binary.BigEndian.AppendUint32(block, length)
	return append(block, data...)
}

func shortDescriptionFixture() []byte {
	block := binary.BigEndian.AppendUint32(nil, 3)
	block = binary.BigEndian.AppendUint32(block, 0)
	return binary.BigEndian.AppendUint32(block, 4<<20)
}

func appendFLACBlock(file []byte, kind byte, body []byte) []byte {
	length := len(body)
	file = append(file, kind, byte(length>>16), byte(length>>8), byte(length))
	return append(file, body...)
}

func vorbisFixture(comments ...string) []byte {
	packet := binary.LittleEndian.AppendUint32(nil, 0)
	packet = binary.LittleEndian.AppendUint32(packet, uint32(len(comments)))
	for _, comment := range comments {
		packet = binary.LittleEndian.AppendUint32(packet, uint32(len(comment)))
		packet = append(packet, comment...)
	}
	return packet
}
