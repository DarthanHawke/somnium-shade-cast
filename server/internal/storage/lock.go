package storage

import (
	"fmt"
	"os"
)

const storageLockName = ".ssc-storage.lock"

// LockRoot берёт эксклюзивную блокировку каталога для процесса сервера.
// Вызывающий код освобождает её через UnlockRoot после остановки доступа к БД.
// Файл блокировки остаётся в каталоге, чтобы все процессы блокировали один файл.
func (s *BlobStore) LockRoot() (*os.File, error) {
	file, err := os.OpenFile(s.blobPath(storageLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("storage: open lock file: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("storage: acquire exclusive lock for %q: %w", s.root, err)
	}
	return file, nil
}

// UnlockRoot ждёт завершения текущих операций записи и освобождает блокировку.
func (s *BlobStore) UnlockRoot(lock *os.File) error {
	s.mutations.Lock()
	defer s.mutations.Unlock()
	return lock.Close()
}
