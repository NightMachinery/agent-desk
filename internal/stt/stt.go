// Package stt streams microphone audio to a transcription backend.
package stt

import (
	"context"
	"encoding/binary"
	"io"
)

const (
	SampleRate     = 16000
	BytesPerSecond = 2 * SampleRate
	Format         = "audio/pcm;rate=16000"
	MaxChunk       = 32 * 1024
	MaxAudio       = 30 * 60 * BytesPerSecond
	WAVHeaderSize  = 44
)

// Backend reads signed little-endian PCM, mono, at 16 kHz. It can report the
// current provisional transcript before returning its final transcript.
type Backend func(ctx context.Context, audio io.Reader, partial func(string)) (string, error)

// WAVHeader describes a PCM recording of size audio bytes.
func WAVHeader(size int64) []byte {
	h := make([]byte, WAVHeaderSize)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(size+36))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], SampleRate)
	binary.LittleEndian.PutUint32(h[28:], BytesPerSecond)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(size))
	return h
}
