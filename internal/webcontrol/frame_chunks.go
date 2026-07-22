package webcontrol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	frameChunkHeaderSize  = 16
	frameChunkMessageSize = 16 << 10
	frameChunkPayloadSize = frameChunkMessageSize - frameChunkHeaderSize
	maxFrameSize          = 16 << 20
)

var frameChunkMagic = [4]byte{'K', 'V', 'M', '1'}
var errInvalidFrameForChunking = errors.New("invalid frame for chunking")

func sendFrameChunks(frameID uint32, frame []byte, send func([]byte) error) error {
	if len(frame) == 0 {
		return fmt.Errorf("%w: frame is empty", errInvalidFrameForChunking)
	}
	if len(frame) > maxFrameSize {
		return fmt.Errorf("%w: frame is %d bytes, maximum is %d", errInvalidFrameForChunking, len(frame), maxFrameSize)
	}
	chunkCount := (len(frame) + frameChunkPayloadSize - 1) / frameChunkPayloadSize
	if chunkCount > int(^uint16(0)) {
		return fmt.Errorf("%w: frame needs too many chunks", errInvalidFrameForChunking)
	}
	packetBuffer := make([]byte, frameChunkMessageSize)
	for index := 0; index < chunkCount; index++ {
		start := index * frameChunkPayloadSize
		end := min(start+frameChunkPayloadSize, len(frame))
		packet := packetBuffer[:frameChunkHeaderSize+end-start]
		copy(packet[:4], frameChunkMagic[:])
		binary.BigEndian.PutUint32(packet[4:8], frameID)
		binary.BigEndian.PutUint16(packet[8:10], uint16(index))
		binary.BigEndian.PutUint16(packet[10:12], uint16(chunkCount))
		binary.BigEndian.PutUint32(packet[12:16], uint32(len(frame)))
		copy(packet[frameChunkHeaderSize:], frame[start:end])
		if err := send(packet); err != nil {
			return err
		}
	}
	return nil
}
