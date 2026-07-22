package capture

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeFrameReader struct {
	nextIndex  uint32
	waits      int
	released   []uint32
	empty      bool
	waitErr    error
	readErr    error
	releaseErr error
}

func (reader *fakeFrameReader) WaitForFrame(uint32) error {
	reader.waits++
	return reader.waitErr
}

func (reader *fakeFrameReader) GetFrame() ([]byte, uint32, error) {
	index := reader.nextIndex
	reader.nextIndex++
	if reader.readErr != nil {
		return nil, index, reader.readErr
	}
	if reader.empty {
		return nil, index, nil
	}
	return []byte{byte(index)}, index, nil
}

func TestDiscardFramesReleasesAnEmptyBufferBeforeFailing(t *testing.T) {
	reader := &fakeFrameReader{empty: true}
	if err := discardFrames(context.Background(), reader, 3, 1); err == nil {
		t.Fatal("discardFrames() error = nil, want empty frame error")
	}
	if !reflect.DeepEqual(reader.released, []uint32{0}) {
		t.Fatalf("released = %v, want [0]", reader.released)
	}
}

func (reader *fakeFrameReader) ReleaseFrame(index uint32) error {
	reader.released = append(reader.released, index)
	return reader.releaseErr
}

func TestDiscardFramesPreservesFailureStage(t *testing.T) {
	tests := []struct {
		name   string
		reader *fakeFrameReader
		want   error
	}{
		{name: "wait", reader: &fakeFrameReader{waitErr: errors.New("wait failed")}, want: errFrameWait},
		{name: "read", reader: &fakeFrameReader{readErr: errors.New("read failed")}, want: errFrameRead},
		{name: "release", reader: &fakeFrameReader{releaseErr: errors.New("release failed")}, want: errFrameRelease},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := discardFrames(context.Background(), test.reader, 3, 1)
			if !errors.Is(err, test.want) {
				t.Fatalf("discardFrames() error = %v, want stage %v", err, test.want)
			}
		})
	}
}

func TestDiscardFramesConsumesAndReleasesEveryWarmupFrame(t *testing.T) {
	reader := &fakeFrameReader{}
	if err := discardFrames(context.Background(), reader, 3, 3); err != nil {
		t.Fatalf("discardFrames(): %v", err)
	}
	if reader.waits != 3 {
		t.Fatalf("wait calls = %d, want 3", reader.waits)
	}
	if !reflect.DeepEqual(reader.released, []uint32{0, 1, 2}) {
		t.Fatalf("released = %v, want [0 1 2]", reader.released)
	}
}

func TestDiscardFramesStopsBeforeReadingWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &fakeFrameReader{}
	if err := discardFrames(ctx, reader, 3, 3); err == nil {
		t.Fatal("discardFrames() error = nil, want context cancellation")
	}
	if reader.waits != 0 || len(reader.released) != 0 {
		t.Fatalf("reader used after cancellation: waits=%d released=%v", reader.waits, reader.released)
	}
}

func TestSupportsExactDimension(t *testing.T) {
	tests := []struct {
		name                   string
		minimum, maximum, step uint32
		target                 uint32
		want                   bool
	}{
		{name: "discrete exact", minimum: 1920, maximum: 1920, target: 1920, want: true},
		{name: "outside range", minimum: 640, maximum: 1280, step: 1, target: 1920, want: false},
		{name: "stepwise aligned", minimum: 640, maximum: 1920, step: 16, target: 1920, want: true},
		{name: "stepwise unaligned", minimum: 641, maximum: 1920, step: 16, target: 1920, want: false},
		{name: "continuous range", minimum: 640, maximum: 3840, step: 0, target: 1920, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := supportsExactDimension(test.minimum, test.maximum, test.step, test.target); got != test.want {
				t.Fatalf("supportsExactDimension(%d, %d, %d, %d) = %v, want %v", test.minimum, test.maximum, test.step, test.target, got, test.want)
			}
		})
	}
}
