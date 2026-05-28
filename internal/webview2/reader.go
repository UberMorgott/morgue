package webview2

import "io"

// progressReader wraps an io.Reader and reports read progress via a callback.
type progressReader struct {
	r        io.Reader
	total    int64
	read     int64
	onUpdate func(read, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	if pr.onUpdate != nil {
		pr.onUpdate(pr.read, pr.total)
	}
	return n, err
}
