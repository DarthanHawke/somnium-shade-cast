// Copyright 2015, David Howden
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tag

import (
	"errors"
	"io"
)

// blockType is a type which represents an enumeration of valid FLAC blocks
type blockType byte

// FLAC block types.
const (
	// Stream Info Block           0
	// Padding Block               1
	// Application Block           2
	// Seektable Block             3
	// Cue Sheet Block             5
	vorbisCommentBlock blockType = 4
	pictureBlock       blockType = 6
)

// ReadFLACTags reads FLAC metadata from the io.ReadSeeker, returning the resulting
// metadata in a Metadata implementation, or non-nil error if there was a problem.
func ReadFLACTags(r io.ReadSeeker) (Metadata, error) {
	flac, err := readString(r, 4)
	if err != nil {
		return nil, err
	}
	if flac != "fLaC" {
		return nil, errors.New("expected 'fLaC'")
	}

	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}

	m := &metadataFLAC{
		newMetadataVorbis(),
	}

	for {
		last, err := m.readFLACMetadataBlock(r, end)
		if err != nil {
			return nil, err
		}

		if last {
			break
		}
	}
	return m, nil
}

type metadataFLAC struct {
	*metadataVorbis
}

func (m *metadataFLAC) readFLACMetadataBlock(r io.ReadSeeker, end int64) (last bool, err error) {
	blockHeader, err := readBytes(r, 1)
	if err != nil {
		return
	}

	if getBit(blockHeader[0], 7) {
		blockHeader[0] ^= (1 << 7)
		last = true
	}

	blockLen, err := readInt(r, 3)
	if err != nil {
		return
	}

	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return last, err
	}
	if int64(blockLen) > end-start {
		return last, io.ErrUnexpectedEOF
	}

	block := &io.LimitedReader{R: r, N: int64(blockLen)}
	switch blockType(blockHeader[0]) {
	case vorbisCommentBlock:
		if err := m.readVorbisComment(block); err != nil {
			return last, err
		}
	case pictureBlock:
		// A damaged or oversized picture must not discard the text tags.
		_ = m.readPictureBlock(block)
	}

	// The block boundary is known even if the picture could not be parsed.
	_, err = r.Seek(start+int64(blockLen), io.SeekStart)
	return last, err
}

func (m *metadataFLAC) FileType() FileType {
	return FLAC
}
