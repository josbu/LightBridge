package handler

import (
	"bytes"
	"sync"

	"github.com/gin-gonic/gin"
)

type opsCaptureWriter struct {
	gin.ResponseWriter
	limit int
	buf   bytes.Buffer
}

const opsCaptureWriterLimit = 64 * 1024

var opsCaptureWriterPool = sync.Pool{
	New: func() any {
		return &opsCaptureWriter{limit: opsCaptureWriterLimit}
	},
}

func acquireOpsCaptureWriter(rw gin.ResponseWriter) *opsCaptureWriter {
	w, ok := opsCaptureWriterPool.Get().(*opsCaptureWriter)
	if !ok || w == nil {
		w = &opsCaptureWriter{}
	}
	w.ResponseWriter = rw
	w.limit = opsCaptureWriterLimit
	w.buf.Reset()
	return w
}

func releaseOpsCaptureWriter(w *opsCaptureWriter) {
	if w == nil {
		return
	}
	w.ResponseWriter = nil
	w.limit = opsCaptureWriterLimit
	w.buf.Reset()
	opsCaptureWriterPool.Put(w)
}

func (w *opsCaptureWriter) Write(b []byte) (int, error) {
	if w.Status() >= 400 && w.limit > 0 && w.buf.Len() < w.limit {
		remaining := w.limit - w.buf.Len()
		if len(b) > remaining {
			_, _ = w.buf.Write(b[:remaining])
		} else {
			_, _ = w.buf.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *opsCaptureWriter) WriteString(s string) (int, error) {
	if w.Status() >= 400 && w.limit > 0 && w.buf.Len() < w.limit {
		remaining := w.limit - w.buf.Len()
		if len(s) > remaining {
			_, _ = w.buf.WriteString(s[:remaining])
		} else {
			_, _ = w.buf.WriteString(s)
		}
	}
	return w.ResponseWriter.WriteString(s)
}
