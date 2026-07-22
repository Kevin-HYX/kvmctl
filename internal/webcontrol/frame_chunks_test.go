package webcontrol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

type frameChunk struct {
	FrameID    uint32
	ChunkIndex uint16
	ChunkCount uint16
	FrameSize  uint32
	Payload    []byte
}

func parseFrameChunk(packet []byte) (frameChunk, error) {
	if len(packet) < frameChunkHeaderSize || len(packet) > frameChunkMessageSize {
		return frameChunk{}, errors.New("invalid frame chunk length")
	}
	if !bytes.Equal(packet[:4], frameChunkMagic[:]) {
		return frameChunk{}, errors.New("invalid frame chunk magic")
	}
	chunk := frameChunk{
		FrameID:    binary.BigEndian.Uint32(packet[4:8]),
		ChunkIndex: binary.BigEndian.Uint16(packet[8:10]),
		ChunkCount: binary.BigEndian.Uint16(packet[10:12]),
		FrameSize:  binary.BigEndian.Uint32(packet[12:16]),
		Payload:    packet[frameChunkHeaderSize:],
	}
	if chunk.FrameSize == 0 || chunk.FrameSize > maxFrameSize || chunk.ChunkCount == 0 || chunk.ChunkIndex >= chunk.ChunkCount {
		return frameChunk{}, errors.New("invalid frame chunk metadata")
	}
	expectedCount := (uint64(chunk.FrameSize) + frameChunkPayloadSize - 1) / frameChunkPayloadSize
	if uint64(chunk.ChunkCount) != expectedCount {
		return frameChunk{}, errors.New("frame chunk count does not match frame size")
	}
	start := uint64(chunk.ChunkIndex) * frameChunkPayloadSize
	expectedPayloadSize := min(uint64(frameChunkPayloadSize), uint64(chunk.FrameSize)-start)
	if uint64(len(chunk.Payload)) != expectedPayloadSize {
		return frameChunk{}, errors.New("frame chunk payload length does not match metadata")
	}
	return chunk, nil
}

func TestSendFrameChunksBoundsMessagesAndReassemblesLargeFrame(t *testing.T) {
	jpeg := make([]byte, 1_524_251)
	jpeg[0], jpeg[1] = 0xff, 0xd8
	jpeg[len(jpeg)-2], jpeg[len(jpeg)-1] = 0xff, 0xd9
	for index := 2; index < len(jpeg)-2; index++ {
		jpeg[index] = byte(index * 31)
	}

	var packets [][]byte
	err := sendFrameChunks(42, jpeg, func(packet []byte) error {
		packets = append(packets, append([]byte(nil), packet...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 2 {
		t.Fatalf("packet count = %d, want a fragmented frame", len(packets))
	}

	chunks := make([][]byte, len(packets))
	for _, packet := range packets {
		if len(packet) > frameChunkMessageSize {
			t.Fatalf("packet length = %d, want at most %d", len(packet), frameChunkMessageSize)
		}
		chunk, err := parseFrameChunk(packet)
		if err != nil {
			t.Fatal(err)
		}
		if chunk.FrameID != 42 || int(chunk.ChunkCount) != len(packets) || chunk.FrameSize != uint32(len(jpeg)) {
			t.Fatalf("unexpected chunk metadata: %#v", chunk)
		}
		chunks[chunk.ChunkIndex] = append([]byte(nil), chunk.Payload...)
	}

	if got := bytes.Join(chunks, nil); !bytes.Equal(got, jpeg) {
		t.Fatalf("reassembled frame differs: got %d bytes, want %d", len(got), len(jpeg))
	}
}

func TestSendFrameChunksStopsAfterSendFailure(t *testing.T) {
	want := errors.New("send failed")
	calls := 0
	err := sendFrameChunks(7, make([]byte, frameChunkPayloadSize*3), func([]byte) error {
		calls++
		if calls == 2 {
			return want
		}
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if calls != 2 {
		t.Fatalf("send calls = %d, want 2", calls)
	}
}

func TestSendFrameChunksRejectsInvalidFramesBeforeSending(t *testing.T) {
	for _, frame := range [][]byte{nil, make([]byte, maxFrameSize+1)} {
		calls := 0
		err := sendFrameChunks(1, frame, func([]byte) error {
			calls++
			return nil
		})
		if !errors.Is(err, errInvalidFrameForChunking) {
			t.Fatalf("error = %v, want errInvalidFrameForChunking", err)
		}
		if calls != 0 {
			t.Fatalf("send calls = %d, want 0", calls)
		}
	}
}

func TestParseFrameChunkRejectsMalformedMetadata(t *testing.T) {
	packets := [][]byte{
		nil,
		make([]byte, frameChunkHeaderSize-1),
		make([]byte, frameChunkHeaderSize),
	}
	for index, packet := range packets {
		if _, err := parseFrameChunk(packet); err == nil {
			t.Fatalf("packet %d parsed successfully, want an error", index)
		}
	}
}

func BenchmarkSendFrameChunks(b *testing.B) {
	for _, size := range []int{205 << 10, 1_524_251} {
		b.Run(fmt.Sprintf("%d_bytes", size), func(b *testing.B) {
			frame := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				if err := sendFrameChunks(uint32(index), frame, func([]byte) error { return nil }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
