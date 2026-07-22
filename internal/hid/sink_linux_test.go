//go:build linux

package hid

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNonblockingHIDWriteTimesOutWhenHostNeverConsumes(t *testing.T) {
	reader, writer := fullNonblockingPipe(t)
	defer unix.Close(reader)
	defer unix.Close(writer)

	startedAt := time.Now()
	err := writeNonblockingReport(writer, []byte{1, 2, 3, 4}, 30*time.Millisecond, unix.Poll)
	elapsed := time.Since(startedAt)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("write error = %v, want an explicit timeout", err)
	}
	if elapsed < 20*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("timeout elapsed = %s, want a bounded wait near 30ms", elapsed)
	}
}

func TestNonblockingHIDWriteRetriesInterruptedPoll(t *testing.T) {
	reader, writer := fullNonblockingPipe(t)
	defer unix.Close(reader)
	defer unix.Close(writer)

	pollCalls := 0
	poll := func(descriptors []unix.PollFd, timeout int) (int, error) {
		pollCalls++
		if pollCalls == 1 {
			return -1, unix.EINTR
		}
		return 0, nil
	}
	err := writeNonblockingReport(writer, []byte{1, 2, 3, 4}, 30*time.Millisecond, poll)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("write error = %v, want timeout after EINTR retry", err)
	}
	if pollCalls != 2 {
		t.Fatalf("poll calls = %d, want EINTR followed by one retry", pollCalls)
	}
}

func TestNonblockingHIDWriteCompletesWhenHostResumes(t *testing.T) {
	reader, writer := fullNonblockingPipe(t)
	defer unix.Close(reader)
	defer unix.Close(writer)

	drained := make(chan error, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		buffer := make([]byte, 8192)
		_, err := unix.Read(reader, buffer)
		drained <- err
	}()
	if err := writeNonblockingReport(writer, []byte{1, 2, 3, 4}, 500*time.Millisecond, unix.Poll); err != nil {
		t.Fatalf("write after host resumed = %v", err)
	}
	if err := <-drained; err != nil && !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("drain pipe: %v", err)
	}
}

func fullNonblockingPipe(t *testing.T) (reader int, writer int) {
	t.Helper()
	descriptors := []int{0, 0}
	if err := unix.Pipe2(descriptors, unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	for {
		if _, err := unix.Write(descriptors[1], buffer); err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				return descriptors[0], descriptors[1]
			}
			unix.Close(descriptors[0])
			unix.Close(descriptors[1])
			t.Fatal(err)
		}
	}
}
