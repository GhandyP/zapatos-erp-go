package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// JSONSnapshot is the result of preflighting one typed JSON collection. The
// original bytes are retained so a later migration step can preserve its input
// exactly, including formatting and a valid empty array.
type JSONSnapshot[T any] struct {
	Collection    string
	Items         []T
	OriginalBytes []byte
	FileExisted   bool
}

// PreflightJSONSnapshot loads a strict JSON array from dataDir/filename. Seeds
// are used only when the file is missing; an existing empty array stays empty.
// The ID selector is used to reject empty and duplicate IDs before migration.
func PreflightJSONSnapshot[T any](dataDir, filename, collection string, seeds []T, idSelector func(T) string) (JSONSnapshot[T], error) {
	snapshot := JSONSnapshot[T]{Collection: collection}
	if strings.TrimSpace(dataDir) == "" {
		return snapshot, errors.New("data directory must not be empty")
	}
	if err := validateSafeName("filename", filename); err != nil {
		return snapshot, err
	}
	if err := validateSafeName("collection", collection); err != nil {
		return snapshot, err
	}
	if idSelector == nil {
		return snapshot, errors.New("ID selector must not be nil")
	}

	dataDirInfo, err := os.Stat(dataDir)
	if err != nil {
		return snapshot, fmt.Errorf("inspect data directory %q: %w", dataDir, err)
	}
	if !dataDirInfo.IsDir() {
		return snapshot, fmt.Errorf("data directory %q is not a directory", dataDir)
	}

	snapshotPath := filepath.Join(dataDir, filename)
	fileInfo, err := os.Lstat(snapshotPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return snapshot, fmt.Errorf("inspect snapshot %q: %w", filename, err)
		}
		snapshot.Items = append([]T(nil), seeds...)
		if err := validateSnapshotIDs(snapshot.Items, idSelector, "seed"); err != nil {
			snapshot.Items = nil
			return snapshot, err
		}
		return snapshot, nil
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return snapshot, fmt.Errorf("snapshot %q must not be a symbolic link", filename)
	}
	if !fileInfo.Mode().IsRegular() {
		return snapshot, fmt.Errorf("snapshot %q is not a regular file", filename)
	}
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return snapshot, fmt.Errorf("read snapshot %q: %w", filename, err)
	}

	snapshot.FileExisted = true
	snapshot.OriginalBytes = append([]byte(nil), data...)
	fail := func(err error) (JSONSnapshot[T], error) {
		snapshot.Items = nil
		return snapshot, err
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fail(fmt.Errorf("snapshot %q is whitespace-only", filename))
	}
	if trimmed[0] != '[' {
		return fail(fmt.Errorf("snapshot %q must contain a JSON array", filename))
	}

	if err := rejectDuplicateJSONKeys(data); err != nil {
		return fail(fmt.Errorf("decode snapshot %q: %w", filename, err))
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var items []T
	if err := decoder.Decode(&items); err != nil {
		return fail(fmt.Errorf("decode snapshot %q: %w", filename, err))
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fail(fmt.Errorf("snapshot %q contains a trailing JSON value", filename))
		}
		return fail(fmt.Errorf("snapshot %q has trailing data: %w", filename, err))
	}
	if items == nil {
		return fail(fmt.Errorf("snapshot %q must contain a JSON array, not null", filename))
	}
	if err := validateSnapshotIDs(items, idSelector, "snapshot"); err != nil {
		return fail(fmt.Errorf("validate snapshot %q: %w", filename, err))
	}
	snapshot.Items = items
	return snapshot, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}

	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object member name is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("malformed JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("malformed JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	return nil
}

func validateSnapshotIDs[T any](items []T, idSelector func(T) string, source string) error {
	seen := make(map[string]struct{}, len(items))
	for index, item := range items {
		id := idSelector(item)
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%s item %d has an empty ID", source, index)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("%s contains duplicate ID %q", source, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// RawBackupSource is one named file to preserve in an immutable backup. An
// absent source is recorded in the manifest but does not produce a content file.
type RawBackupSource struct {
	Name   string
	Data   []byte
	Exists bool
}

// BackupManifestFilename is reserved for the deterministic backup manifest.
const BackupManifestFilename = "manifest.json"

type backupManifest struct {
	Files []backupManifestEntry `json:"files"`
}

type backupManifestEntry struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256,omitempty"`
}

type preparedBackupSource struct {
	RawBackupSource
}

// CreateImmutableBackup atomically publishes exact source bytes and a
// deterministic SHA-256 manifest under backupDir. Existing backups are reused
// only after every entry, byte, and private permission has been verified; other
// existing destinations are never overwritten. The caller must ensure that
// backupDir's parent directory already exists; this function does not create it.
func CreateImmutableBackup(backupDir string, sources []RawBackupSource) (retErr error) {
	if strings.TrimSpace(backupDir) == "" {
		return errors.New("backup directory must not be empty")
	}

	prepared := make([]preparedBackupSource, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	manifest := backupManifest{Files: make([]backupManifestEntry, 0, len(sources))}
	for _, source := range sources {
		if err := validateSafeName("backup source name", source.Name); err != nil {
			return err
		}
		if strings.EqualFold(source.Name, BackupManifestFilename) {
			return fmt.Errorf("backup source name %q is reserved", source.Name)
		}
		key := strings.ToLower(source.Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate backup source name %q", source.Name)
		}
		seen[key] = struct{}{}
		if !source.Exists && len(source.Data) != 0 {
			return fmt.Errorf("missing backup source %q has content bytes", source.Name)
		}

		source.Data = append([]byte(nil), source.Data...)
		entry := backupManifestEntry{Name: source.Name, Exists: source.Exists}
		if source.Exists {
			digest := sha256.Sum256(source.Data)
			entry.SHA256 = hex.EncodeToString(digest[:])
		}
		manifest.Files = append(manifest.Files, entry)
		prepared = append(prepared, preparedBackupSource{RawBackupSource: source})
	}
	sort.Slice(prepared, func(i, j int) bool { return prepared[i].Name < prepared[j].Name })
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Name < manifest.Files[j].Name })
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}

	info, err := os.Lstat(backupDir)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup destination %q is not a directory", backupDir)
		}
		return verifyImmutableBackup(backupDir, prepared, manifestBytes)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup destination %q: %w", backupDir, err)
	}

	parentDir := filepath.Dir(backupDir)
	tempDir, err := os.MkdirTemp(parentDir, "."+filepath.Base(backupDir)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary backup directory under %q: %w", parentDir, err)
	}
	published := false
	defer func() {
		if !published {
			if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("remove temporary backup directory %q: %w", tempDir, cleanupErr))
			}
		}
	}()
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return fmt.Errorf("set temporary backup directory permissions: %w", err)
	}
	for _, source := range prepared {
		if !source.Exists {
			continue
		}
		if err := writeNewPrivateFile(filepath.Join(tempDir, source.Name), source.Data); err != nil {
			return fmt.Errorf("write backup source %q: %w", source.Name, err)
		}
	}
	if err := writeNewPrivateFile(filepath.Join(tempDir, BackupManifestFilename), manifestBytes); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err := syncDirectory(tempDir); err != nil {
		return fmt.Errorf("sync temporary backup directory: %w", err)
	}
	if err := os.Rename(tempDir, backupDir); err != nil {
		info, inspectErr := os.Lstat(backupDir)
		if inspectErr == nil {
			if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return verifyImmutableBackup(backupDir, prepared, manifestBytes)
			}
			return fmt.Errorf("backup destination %q appeared during publication and is not a directory", backupDir)
		}
		if !errors.Is(inspectErr, os.ErrNotExist) {
			return errors.Join(fmt.Errorf("publish backup %q: %w", backupDir, err), fmt.Errorf("inspect backup destination after publication failure: %w", inspectErr))
		}
		return fmt.Errorf("publish backup %q: %w", backupDir, err)
	}
	published = true
	if err := syncDirectory(parentDir); err != nil {
		return fmt.Errorf("sync backup parent directory %q: %w", parentDir, err)
	}
	return nil
}

func verifyImmutableBackup(backupDir string, sources []preparedBackupSource, manifestBytes []byte) error {
	dirInfo, err := os.Lstat(backupDir)
	if err != nil {
		return fmt.Errorf("inspect existing backup directory: %w", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || dirInfo.Mode().Perm() != 0o700 {
		return fmt.Errorf("existing backup directory %q is not a private 0700 directory", backupDir)
	}

	expected := make(map[string][]byte, len(sources)+1)
	for _, source := range sources {
		if source.Exists {
			expected[source.Name] = source.Data
		}
	}
	expected[BackupManifestFilename] = manifestBytes

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("read existing backup directory: %w", err)
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("existing backup directory %q is partial or contains unexpected files", backupDir)
	}
	for _, entry := range entries {
		want, exists := expected[entry.Name()]
		if !exists {
			return fmt.Errorf("existing backup contains unexpected file %q", entry.Name())
		}
		path := filepath.Join(backupDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect existing backup file %q: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return fmt.Errorf("existing backup file %q is not a private 0600 regular file", entry.Name())
		}
		got, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read existing backup file %q: %w", entry.Name(), err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("existing backup file %q conflicts with requested backup", entry.Name())
		}
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}

func writeNewPrivateFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Chmod(0o600)
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func validateSafeName(label, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s must not be empty", label)
	}
	if name == "." || name == ".." || filepath.IsAbs(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || strings.IndexByte(name, 0) >= 0 {
		return fmt.Errorf("%s %q must be a safe basename", label, name)
	}
	return nil
}
