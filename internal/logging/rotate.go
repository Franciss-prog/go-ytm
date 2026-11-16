package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RotatingWriter is an io.WriteCloser that rotates path → path.1 → … →
// path.N when the file would exceed maxBytes. Safe for concurrent use.
type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	f        *os.File
	size     int64
}

// NewRotatingWriter opens (appending) or creates path.
func NewRotatingWriter(path string, maxBytes int64, backups int) (*RotatingWriter, error) {
	if maxBytes <= 0 {
		maxBytes = 5 << 20
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	w := &RotatingWriter{path: path, maxBytes: maxBytes, backups: max(backups, 0)}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

// Write appends p, rotating first if needed.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, fmt.Errorf("rotate log: %w", err)
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) rotate() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	w.f = nil
	if w.backups == 0 {
		if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return w.open()
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.backups))
	for i := w.backups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, fmt.Sprintf("%s.%d", w.path, i+1)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		return err
	}
	return w.open()
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
