package hid

type Sink interface {
	Write([]byte) error
}

type FileSink struct {
	Path string
}

func (sink FileSink) Write(report []byte) error {
	return writeFileReport(sink.Path, report)
}
