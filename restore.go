package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// restoreBackup распаковывает резервную копию в каталог данных.
// Служба должна быть остановлена: иначе запущенный сервер перезапишет файлы.
func restoreBackup(dataDir, zipPath string, force bool) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("не удалось открыть архив: %w", err)
	}
	defer zr.Close()

	if !force {
		if _, err := os.Stat(filepath.Join(dataDir, "users.json")); err == nil {
			return errors.New("в папке данных уже есть users.json; остановите службу и добавьте -force")
		}
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "chats"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "uploads"), 0o755); err != nil {
		return err
	}

	restored := 0
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if f.FileInfo().IsDir() {
			continue
		}
		var target string
		switch {
		case name == "users.json" || name == "channels.json" || name == "uploads.json":
			target = filepath.Join(dataDir, name)
		case strings.HasPrefix(name, "chats/") && strings.HasSuffix(name, ".json"):
			base := filepath.Base(name)
			if base == "." || base == ".." {
				continue
			}
			target = filepath.Join(dataDir, "chats", base)
		case strings.HasPrefix(name, "uploads/"):
			base := filepath.Base(name)
			if base == "." || base == ".." {
				continue
			}
			target = filepath.Join(dataDir, "uploads", base)
		default:
			continue
		}
		if err := extractZipFile(f, target); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		restored++
	}
	if restored == 0 {
		return errors.New("в архиве нет файлов Офис-чата")
	}
	// Вложения лежат не в архиве, а зеркалом рядом с ним (backups/uploads).
	// Если папка на месте, дополняем uploads, не трогая уже существующие файлы.
	if err := copyMissing(filepath.Join(filepath.Dir(zipPath), "uploads"), filepath.Join(dataDir, "uploads")); err != nil {
		return err
	}
	return nil
}

// copyMissing копирует файлы из src в dst, пропуская уже существующие.
func copyMissing(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		target := filepath.Join(dst, e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		}
		in, err := os.Open(filepath.Join(src, e.Name()))
		if err != nil {
			continue
		}
		tmp := target + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			_ = in.Close()
			continue
		}
		_, cerr := io.Copy(out, in)
		_ = in.Close()
		if err := out.Close(); err != nil || cerr != nil {
			_ = os.Remove(tmp)
			continue
		}
		_ = os.Rename(tmp, target)
	}
	return nil
}

func extractZipFile(f *zip.File, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := target + ".tmp"
	dst, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}
