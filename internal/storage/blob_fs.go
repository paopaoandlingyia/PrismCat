package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrRefSetEmpty 表示引用列表为空,但仓库里确实有 blob。
//
// blob 目录的路径不是从数据库路径推导的,两者只是约定配对。一旦进程被指向
// 另一个数据库(还原备份、换库测试、配置写错),引用列表就会是空的,而照字面
// 执行 GC 会把整个仓库删空 —— 那些 blob 属于原来那个库,不是孤儿。
//
// 代价完全不对等:误删是静默且永久的数据丢失,误留只是占点磁盘。所以这里宁可
// 不删。真的想清空,先把日志删掉让仓库自然变空,或者手动删目录。
var ErrRefSetEmpty = errors.New("blob gc: reference set is empty but the store is not; refusing to delete")

// FileBlobStore stores blobs on the local filesystem under a content-addressed path.
// Layout: <baseDir>/<hash[:2]>/<hash>
type FileBlobStore struct {
	baseDir string
}

func NewFileBlobStore(baseDir string) (*FileBlobStore, error) {
	if baseDir == "" {
		return nil, errors.New("blob base dir is empty")
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &FileBlobStore{baseDir: baseDir}, nil
}

func (s *FileBlobStore) Put(ctx context.Context, data []byte) (string, error) {
	_ = ctx

	sum := sha256.Sum256(data)
	ref := newSHA256Ref(sum)
	_, hexHash, _ := parseBlobRef(ref)

	finalPath := s.pathFor(hexHash)
	if _, err := os.Stat(finalPath); err == nil {
		return ref, nil
	}

	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	tmpPath := filepath.Join(dir, ".tmp-"+hexHash+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", err
	}

	// Rename is atomic on the same filesystem.
	if err := os.Rename(tmpPath, finalPath); err != nil {
		// If another writer won the race, keep the existing blob.
		if _, statErr := os.Stat(finalPath); statErr == nil {
			_ = os.Remove(tmpPath)
			return ref, nil
		}
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("store blob: %w", err)
	}

	return ref, nil
}

func (s *FileBlobStore) Get(ctx context.Context, ref string) ([]byte, error) {
	_ = ctx
	_, hexHash, err := parseBlobRef(ref)
	if err != nil {
		return nil, err
	}
	path := s.pathFor(hexHash)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	return b, nil
}

func (s *FileBlobStore) Exists(ctx context.Context, ref string) (bool, error) {
	_ = ctx
	_, hexHash, err := parseBlobRef(ref)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(s.pathFor(hexHash))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GarbageCollect removes unreferenced blob files.
// referencedRefs should contain canonical refs stored in the log table (e.g. "sha256:<hex>").
// minAge avoids deleting blobs created very recently (to reduce races with in-flight log writes).
// isEmpty 报告仓库里是否一个 blob 文件都没有。命中第一个就返回,不遍历全部。
func (s *FileBlobStore) isEmpty() (bool, error) {
	found := false
	errFound := errors.New("found")
	err := filepath.WalkDir(s.baseDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isBlobFileName(d.Name()) {
			return nil
		}
		found = true
		return errFound
	})
	if errors.Is(err, errFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return !found, nil
}

// isBlobFileName 判断一个文件名是否是内容寻址的 blob(而非 .tmp- 临时文件)。
func isBlobFileName(name string) bool {
	if strings.HasPrefix(name, ".tmp-") || len(name) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

func (s *FileBlobStore) GarbageCollect(ctx context.Context, referencedRefs []string, minAge time.Duration) (int, error) {
	_ = ctx

	referenced := make(map[string]struct{}, len(referencedRefs))
	for _, ref := range referencedRefs {
		_, hexHash, err := parseBlobRef(ref)
		if err != nil {
			continue
		}
		referenced[hexHash] = struct{}{}
	}

	// 空引用列表 + 非空仓库 = 库和 blob 仓库对不上,而不是全都成了孤儿
	if len(referenced) == 0 {
		empty, err := s.isEmpty()
		if err != nil {
			return 0, err
		}
		if !empty {
			return 0, ErrRefSetEmpty
		}
	}

	var cutoff time.Time
	if minAge > 0 {
		cutoff = time.Now().Add(-minAge)
	}

	deleted := 0
	err := filepath.WalkDir(s.baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		name := d.Name()
		if !isBlobFileName(name) {
			return nil
		}
		if _, ok := referenced[name]; ok {
			return nil
		}

		if !cutoff.IsZero() {
			info, err := d.Info()
			if err == nil && info.ModTime().After(cutoff) {
				return nil
			}
		}

		if err := os.Remove(path); err == nil {
			deleted++
		}
		return nil
	})
	if err != nil {
		return deleted, err
	}

	// Best-effort: remove empty prefix directories.
	entries, err := os.ReadDir(s.baseDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			_ = os.Remove(filepath.Join(s.baseDir, e.Name()))
		}
	}

	return deleted, nil
}

func (s *FileBlobStore) pathFor(hexHash string) string {
	prefix := hexHash[:2]
	return filepath.Join(s.baseDir, prefix, hexHash)
}
