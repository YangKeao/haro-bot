package skillbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	MaxSupportingFiles       = 256
	MaxFiles                 = MaxSupportingFiles + 1
	MaxFileSize        int64 = 1 << 20
	MaxTotalSize       int64 = 8 << 20
	MaxArchiveSize     int64 = 10 << 20
)

var vcsDirectories = map[string]struct{}{`.git`: {}, `.hg`: {}, `.svn`: {}}

type File struct {
	Path       string
	Executable bool
	Size       int64
}

type Manifest struct {
	Hash      string
	Files     []File
	TotalSize int64
}

func BundleDir(baseDir, hash string) string {
	return filepath.Join(baseDir, "bundles", "sha256", hash)
}

func ValidHash(hash string) bool {
	if len(hash) != sha256.Size*2 || strings.ToLower(hash) != hash {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func ValidResourcePath(resource string) bool {
	if resource == "" || strings.Contains(resource, `\`) || strings.HasPrefix(resource, "/") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(resource)))
	return clean == resource && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func Scan(root string) (Manifest, error) {
	root = filepath.Clean(root)
	var files []File
	var total int64
	seenSkill := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				return errors.New("skill root must be a directory, not a symlink")
			}
			return nil
		}
		if _, excluded := vcsDirectories[entry.Name()]; excluded {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill bundle contains symlink: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("skill bundle contains special file: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !ValidResourcePath(rel) {
			return fmt.Errorf("invalid skill resource path: %s", rel)
		}
		if info.Size() > MaxFileSize {
			return fmt.Errorf("skill resource %s exceeds %d bytes", rel, MaxFileSize)
		}
		if len(files) >= MaxFiles {
			return fmt.Errorf("skill bundle exceeds %d files", MaxFiles)
		}
		total += info.Size()
		if total > MaxTotalSize {
			return fmt.Errorf("skill bundle exceeds %d bytes", MaxTotalSize)
		}
		if rel == "SKILL.md" {
			seenSkill = true
		}
		files = append(files, File{Path: rel, Executable: info.Mode().Perm()&0o111 != 0, Size: info.Size()})
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	if !seenSkill {
		return Manifest{}, errors.New("skill bundle is missing SKILL.md")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	hash := sha256.New()
	_, _ = io.WriteString(hash, "haro-skill-bundle-v1\x00")
	for _, file := range files {
		_, _ = io.WriteString(hash, file.Path+"\x00")
		if file.Executable {
			_, _ = io.WriteString(hash, "x\x00")
		} else {
			_, _ = io.WriteString(hash, "r\x00")
		}
		_, _ = io.WriteString(hash, strconv.FormatInt(file.Size, 10)+"\x00")
		in, err := os.Open(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			return Manifest{}, err
		}
		_, copyErr := io.Copy(hash, in)
		closeErr := in.Close()
		if copyErr != nil {
			return Manifest{}, copyErr
		}
		if closeErr != nil {
			return Manifest{}, closeErr
		}
		_, _ = hash.Write([]byte{0})
	}
	return Manifest{Hash: hex.EncodeToString(hash.Sum(nil)), Files: files, TotalSize: total}, nil
}

func Snapshot(baseDir, sourceRoot string) (Manifest, string, error) {
	sourceManifest, err := Scan(sourceRoot)
	if err != nil {
		return Manifest{}, "", err
	}
	parent := filepath.Join(baseDir, "bundles", "sha256")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Manifest{}, "", err
	}
	target := BundleDir(baseDir, sourceManifest.Hash)
	if existing, err := Scan(target); err == nil && existing.Hash == sourceManifest.Hash {
		return existing, target, nil
	}
	temp, err := os.MkdirTemp(parent, ".bundle-")
	if err != nil {
		return Manifest{}, "", err
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0o755); err != nil {
		return Manifest{}, "", err
	}
	for _, file := range sourceManifest.Files {
		src := filepath.Join(sourceRoot, filepath.FromSlash(file.Path))
		dst := filepath.Join(temp, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return Manifest{}, "", err
		}
		if err := copyFile(src, dst, file.Executable); err != nil {
			return Manifest{}, "", err
		}
	}
	copied, err := Scan(temp)
	if err != nil {
		return Manifest{}, "", err
	}
	if copied.Hash != sourceManifest.Hash {
		return Manifest{}, "", errors.New("skill changed while bundle was being created")
	}
	if err := os.RemoveAll(target); err != nil {
		return Manifest{}, "", err
	}
	if err := os.Rename(temp, target); err != nil {
		return Manifest{}, "", err
	}
	return copied, target, nil
}

func Archive(root string) ([]byte, Manifest, error) {
	manifest, err := Scan(root)
	if err != nil {
		return nil, Manifest{}, err
	}
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, file := range manifest.Files {
		mode := int64(0o444)
		if file.Executable {
			mode = 0o555
		}
		header := &tar.Header{Name: file.Path, Mode: mode, Size: file.Size, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			return nil, Manifest{}, err
		}
		in, err := os.Open(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			return nil, Manifest{}, err
		}
		_, copyErr := io.Copy(tw, in)
		closeErr := in.Close()
		if copyErr != nil {
			return nil, Manifest{}, copyErr
		}
		if closeErr != nil {
			return nil, Manifest{}, closeErr
		}
	}
	if err := tw.Close(); err != nil {
		return nil, Manifest{}, err
	}
	if err := gz.Close(); err != nil {
		return nil, Manifest{}, err
	}
	if int64(output.Len()) > MaxArchiveSize {
		return nil, Manifest{}, errors.New("compressed skill bundle is too large")
	}
	return output.Bytes(), manifest, nil
}

func ExtractArchive(input io.Reader, destination string) (Manifest, error) {
	if err := os.Chmod(destination, 0o755); err != nil {
		return Manifest{}, err
	}
	gz, err := gzip.NewReader(io.LimitReader(input, MaxArchiveSize+1))
	if err != nil {
		return Manifest{}, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := make(map[string]struct{})
	files := 0
	var total int64
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return Manifest{}, fmt.Errorf("unsupported archive entry type for %s", header.Name)
		}
		if !ValidResourcePath(header.Name) {
			return Manifest{}, fmt.Errorf("invalid archive path: %s", header.Name)
		}
		if _, duplicate := seen[header.Name]; duplicate {
			return Manifest{}, fmt.Errorf("duplicate archive path: %s", header.Name)
		}
		seen[header.Name] = struct{}{}
		files++
		if files > MaxFiles {
			return Manifest{}, fmt.Errorf("skill bundle exceeds %d files", MaxFiles)
		}
		if header.Size < 0 || header.Size > MaxFileSize {
			return Manifest{}, fmt.Errorf("skill resource %s has invalid size", header.Name)
		}
		total += header.Size
		if total > MaxTotalSize {
			return Manifest{}, fmt.Errorf("skill bundle exceeds %d bytes", MaxTotalSize)
		}
		path := filepath.Join(destination, filepath.FromSlash(header.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Manifest{}, err
		}
		mode := os.FileMode(0o444)
		if header.Mode&0o111 != 0 {
			mode = 0o555
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return Manifest{}, err
		}
		written, copyErr := io.CopyN(out, tr, header.Size)
		closeErr := out.Close()
		if copyErr != nil || written != header.Size {
			return Manifest{}, fmt.Errorf("extract %s: %w", header.Name, copyErr)
		}
		if closeErr != nil {
			return Manifest{}, closeErr
		}
		if err := os.Chmod(path, mode); err != nil {
			return Manifest{}, err
		}
	}
	return Scan(destination)
}

func copyFile(source, destination string, executable bool) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	mode := os.FileMode(0o444)
	if executable {
		mode = 0o555
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, mode)
}
