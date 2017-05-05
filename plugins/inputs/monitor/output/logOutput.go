package output

import (
	"github.com/armon/circbuf"
	"github.com/golang/glog"
)

const (
	// DefaultBufSize  default buf size for LogOutputer
	defaultBufSize = 64 * 1024
)

// LogOutputer a simple io.Writer
type LogOutputer struct {
	buf *circbuf.Buffer
}

// NewLogOutputer returns a LogOutputer
func NewLogOutputer(size int64) *LogOutputer {
	if size <= 0 {
		size = defaultBufSize
	}
	buf, err := circbuf.NewBuffer(size)
	if err != nil {
		glog.Errorf("create new cirbuff: %v", err)
		glog.Fatal("create create cirbuff")
	}
	return &LogOutputer{
		buf: buf,
	}
}

// Output implements Outputer interface
func (op *LogOutputer) Write(data []byte) (int, error) {
	glog.Infof("%q", data)
	op.buf.Write(data)
	return len(data), nil
}

// GetDataString  returns at most size bytes of data lastestly outputed
func (op *LogOutputer) GetDataString() string {
	return op.buf.String()
}

// Reset reset buf
func (op *LogOutputer) Reset() {
	op.buf.Reset()
}
