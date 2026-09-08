package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

var ErrLocked = errors.New("another AgentWarmup operation is running")

type Lock struct{ file *os.File }

func TryLock(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{file: f}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(err, closeErr)
}

func WithLock(ctx context.Context, path string, fn func() error) error {
	for {
		lock, err := TryLock(path)
		if err == nil {
			defer lock.Close()
			return fn()
		}
		if !errors.Is(err, ErrLocked) {
			return err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
