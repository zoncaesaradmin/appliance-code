// Package videomedia validates media formats accepted by the appliance video library.
package videomedia

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const maxSampleDescriptionBytes = 2 << 20

type mp4Box struct {
	typ        string
	payloadAt  int64
	payloadLen int64
}

// ValidateBrowserMP4 accepts the single stored video format used by the appliance:
// an ISO Base Media file with H.264 video and optional AAC audio. Conversion is
// deliberately not performed here; callers receive a validation failure instead.
func ValidateBrowserMP4(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open uploaded video: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat uploaded video: %w", err)
	}
	if info.Size() < 16 {
		return fmt.Errorf("video must be a valid MP4 file with H.264 video")
	}

	var hasFileType bool
	var movie *mp4Box
	err = eachBox(file, 0, info.Size(), func(box mp4Box) error {
		switch box.typ {
		case "ftyp":
			hasFileType = true
		case "moov":
			copy := box
			movie = &copy
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("video must be a valid MP4 file: %w", err)
	}
	if !hasFileType || movie == nil {
		return fmt.Errorf("video must be an MP4 file with movie metadata")
	}

	var videoTracks, validVideoTracks, audioTracks, validAudioTracks int
	err = eachBox(file, movie.payloadAt, movie.payloadAt+movie.payloadLen, func(box mp4Box) error {
		if box.typ != "trak" {
			return nil
		}
		kind, sample, err := readTrackDescription(file, box)
		if err != nil {
			return err
		}
		switch kind {
		case "vide":
			videoTracks++
			if sample.hasOnlyH264() {
				validVideoTracks++
			}
		case "soun":
			audioTracks++
			if sample.hasOnlyAAC() {
				validAudioTracks++
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("read MP4 movie metadata: %w", err)
	}
	if videoTracks == 0 || validVideoTracks != videoTracks {
		return fmt.Errorf("video must use the H.264 (AVC) codec")
	}
	if audioTracks > 0 && validAudioTracks != audioTracks {
		return fmt.Errorf("audio must use the AAC codec")
	}
	return nil
}

type trackSample struct {
	codecs []string
	aac    []bool
}

func (s trackSample) hasOnlyH264() bool {
	if len(s.codecs) == 0 {
		return false
	}
	for _, codec := range s.codecs {
		if codec != "avc1" && codec != "avc3" {
			return false
		}
	}
	return true
}

func (s trackSample) hasOnlyAAC() bool {
	if len(s.codecs) == 0 || len(s.codecs) != len(s.aac) {
		return false
	}
	for index, codec := range s.codecs {
		if codec != "mp4a" || !s.aac[index] {
			return false
		}
	}
	return true
}

func readTrackDescription(file *os.File, track mp4Box) (string, trackSample, error) {
	var handler string
	var sample trackSample
	err := eachBox(file, track.payloadAt, track.payloadAt+track.payloadLen, func(box mp4Box) error {
		if box.typ != "mdia" {
			return nil
		}
		return eachBox(file, box.payloadAt, box.payloadAt+box.payloadLen, func(mediaBox mp4Box) error {
			switch mediaBox.typ {
			case "hdlr":
				payload, err := readPayload(file, mediaBox, 12)
				if err != nil {
					return err
				}
				handler = string(payload[8:12])
			case "minf":
				parsed, err := readSampleDescription(file, mediaBox)
				if err != nil {
					return err
				}
				sample = parsed
			}
			return nil
		})
	})
	if err != nil {
		return "", trackSample{}, err
	}
	if handler == "" {
		return "", trackSample{}, fmt.Errorf("track has no handler metadata")
	}
	if handler == "vide" || handler == "soun" {
		if len(sample.codecs) == 0 {
			return "", trackSample{}, fmt.Errorf("%s track has no sample description", handler)
		}
	}
	return handler, sample, nil
}

func readSampleDescription(file *os.File, mediaInfo mp4Box) (trackSample, error) {
	var result trackSample
	err := eachBox(file, mediaInfo.payloadAt, mediaInfo.payloadAt+mediaInfo.payloadLen, func(box mp4Box) error {
		if box.typ != "stbl" {
			return nil
		}
		return eachBox(file, box.payloadAt, box.payloadAt+box.payloadLen, func(stblBox mp4Box) error {
			if stblBox.typ != "stsd" {
				return nil
			}
			payload, err := readPayload(file, stblBox, 16)
			if err != nil {
				return err
			}
			if len(payload) < 16 || binary.BigEndian.Uint32(payload[4:8]) == 0 {
				return fmt.Errorf("sample description has no entries")
			}
			entryCount := int(binary.BigEndian.Uint32(payload[4:8]))
			entryAt := 8
			for index := 0; index < entryCount; index++ {
				if entryAt+8 > len(payload) {
					return fmt.Errorf("truncated sample description entry")
				}
				entrySize := int(binary.BigEndian.Uint32(payload[entryAt : entryAt+4]))
				if entrySize < 8 || entrySize > len(payload)-entryAt {
					return fmt.Errorf("invalid sample description entry")
				}
				entry := payload[entryAt : entryAt+entrySize]
				codec := string(entry[4:8])
				result.codecs = append(result.codecs, codec)
				result.aac = append(result.aac, codec == "mp4a" && containsAACDescriptor(entry[8:]))
				entryAt += entrySize
			}
			return nil
		})
	})
	return result, err
}

func containsAACDescriptor(data []byte) bool {
	for index := 0; index+2 < len(data); index++ {
		if data[index] != 0x04 {
			continue
		}
		cursor := index + 1
		for count := 0; count < 4 && cursor < len(data); count++ {
			if data[cursor]&0x80 == 0 {
				cursor++
				break
			}
			cursor++
		}
		if cursor < len(data) && data[cursor] == 0x40 { // MPEG-4 AAC object type.
			return true
		}
	}
	return false
}

func eachBox(file *os.File, start, end int64, visit func(mp4Box) error) error {
	for offset := start; offset < end; {
		box, next, err := readBox(file, offset, end)
		if err != nil {
			return err
		}
		if err := visit(box); err != nil {
			return err
		}
		offset = next
	}
	return nil
}

func readBox(file *os.File, offset, end int64) (mp4Box, int64, error) {
	if end-offset < 8 {
		return mp4Box{}, 0, fmt.Errorf("truncated MP4 box")
	}
	var header [16]byte
	if _, err := file.ReadAt(header[:8], offset); err != nil {
		return mp4Box{}, 0, err
	}
	size := int64(binary.BigEndian.Uint32(header[:4]))
	headerLen := int64(8)
	if size == 1 {
		if end-offset < 16 {
			return mp4Box{}, 0, fmt.Errorf("truncated extended MP4 box")
		}
		if _, err := file.ReadAt(header[8:16], offset+8); err != nil {
			return mp4Box{}, 0, err
		}
		if binary.BigEndian.Uint64(header[8:16]) > uint64(^uint64(0)>>1) {
			return mp4Box{}, 0, fmt.Errorf("MP4 box is too large")
		}
		size = int64(binary.BigEndian.Uint64(header[8:16]))
		headerLen = 16
	} else if size == 0 {
		size = end - offset
	}
	if size < headerLen || size > end-offset {
		return mp4Box{}, 0, fmt.Errorf("invalid MP4 box length")
	}
	return mp4Box{typ: string(header[4:8]), payloadAt: offset + headerLen, payloadLen: size - headerLen}, offset + size, nil
}

func readPayload(file *os.File, box mp4Box, minimum int) ([]byte, error) {
	if box.payloadLen < int64(minimum) || box.payloadLen > maxSampleDescriptionBytes {
		return nil, fmt.Errorf("invalid or oversized MP4 metadata")
	}
	payload := make([]byte, box.payloadLen)
	if _, err := file.ReadAt(payload, box.payloadAt); err != nil && err != io.EOF {
		return nil, err
	}
	return payload, nil
}
