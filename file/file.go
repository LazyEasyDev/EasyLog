// Package file provides dated, bounded NDJSON file segments.
package file

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	defaultMaxSegmentBytes int64 = 8 << 20
	defaultMaxSegments           = 7
	defaultPermissions           = 0o644
	maxLineBufferBytes           = 64 * 1024
	dateLayout                   = "20060102"
	logsDirectoryName            = "logs"
	debugPrefix                  = "debug"
	infoPrefix                   = "info"
	warningPrefix                = "warn"
	errorPrefix                  = "err"
)

type levelBucket uint8

const (
	debugBucket levelBucket = iota
	infoBucket
	warningBucket
	errorBucket
)

var levelPrefixes = [...]string{
	debugPrefix,
	infoPrefix,
	warningPrefix,
	errorPrefix,
}

// ErrClosed indicates that the file output has already been closed.
var ErrClosed = errors.New("easylog/file: closed")

// Options configures segmented file output.
type Options struct {
	// Directory is the absolute base path where EasyLog creates its logs subdirectory.
	Directory       string
	MaxSegmentBytes int64
	MaxSegments     int
	Permissions     fs.FileMode
}

type segment struct {
	name   string
	path   string
	date   time.Time
	suffix uint64
}

type levelStore struct {
	directory     string
	prefix        string
	pattern       *regexp.Regexp
	segments      []segment
	resumePending bool
	active        *os.File
	activeSegment segment
	activeBytes   int64
}

// Output writes records to append-only date-and-sequence-named segments.
type Output struct {
	mu sync.Mutex

	options    Options
	now        func() time.Time
	stores     [len(levelPrefixes)]levelStore
	closed     bool
	lineBuffer []byte
}

// New creates a file output under a managed logs directory.
func New(options Options) (*Output, error) {
	return newWithClock(options, time.Now)
}

func newWithClock(options Options, now func() time.Time) (*Output, error) {
	options, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	logsDirectory := filepath.Join(options.Directory, logsDirectoryName)
	if err := os.MkdirAll(logsDirectory, 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	output := &Output{
		options: options,
		now:     now,
	}
	for bucket, prefix := range levelPrefixes {
		store := &output.stores[bucket]
		store.directory = logsDirectory
		store.prefix = prefix
		store.pattern = segmentPattern(prefix)
		if err := output.scanSegments(store); err != nil {
			return nil, err
		}
		store.resumePending = true
	}
	return output, nil
}

// WriteRecord writes one complete record, rotating first when necessary.
// If segment creation fails, the record is dropped; later calls retry creation.
// It returns an error only when the record cannot be fully written.
func (o *Output) WriteRecord(record slog.Record, jsonContent []byte) error {
	lineBytes := int64(len(jsonContent)) + 1
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrClosed
	}

	store := o.storeForLevel(record.Level)
	date := o.currentDate()
	var prepareErr error
	if store.active == nil {
		prepareErr = o.openSegment(store, date)
	}
	dateChanged := store.active != nil && !store.activeSegment.date.Equal(date)
	segmentFull := store.active != nil && store.activeBytes > 0 && store.activeBytes+lineBytes > o.options.MaxSegmentBytes
	if dateChanged || segmentFull {
		rotated, err := o.rotate(store, date)
		prepareErr = errors.Join(prepareErr, err)
		if !rotated {
			return prepareErr
		}
	}
	if store.active == nil {
		return prepareErr
	}
	o.lineBuffer = slices.Grow(o.lineBuffer[:0], len(jsonContent)+1)
	o.lineBuffer = append(o.lineBuffer, jsonContent...)
	o.lineBuffer = append(o.lineBuffer, '\n')
	written, writeErr := writeLine(store.active, o.lineBuffer)
	if cap(o.lineBuffer) > maxLineBufferBytes {
		o.lineBuffer = nil
	}
	store.activeBytes += int64(written)
	if writeErr == nil {
		return nil
	}
	closeErr := store.active.Close()
	store.active = nil
	return errors.Join(prepareErr, writeErr, closeErr)
}

func writeLine(writer io.Writer, data []byte) (int, error) {
	if len(data) == 0 {
		data = []byte{'\n'}
	}
	var total int
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written > 0 {
			total += written
			data = data[written:]
		}
		if err != nil {
			return total, err
		}
		if written == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

// Sync flushes the active segment to stable storage.
func (o *Output) Sync() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrClosed
	}
	var syncErrors []error
	for bucket := range o.stores {
		if active := o.stores[bucket].active; active != nil {
			if err := active.Sync(); err != nil {
				syncErrors = append(syncErrors, fmt.Errorf("%s: %w", levelPrefixes[bucket], err))
			}
		}
	}
	return errors.Join(syncErrors...)
}

// Close idempotently closes the active segment.
func (o *Output) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	var closeErrors []error
	for bucket := range o.stores {
		if active := o.stores[bucket].active; active != nil {
			if err := active.Close(); err != nil {
				closeErrors = append(closeErrors, fmt.Errorf("%s: %w", levelPrefixes[bucket], err))
			}
		}
	}
	return errors.Join(closeErrors...)
}

func normalizeOptions(options Options) (Options, error) {
	if !filepath.IsAbs(options.Directory) {
		return Options{}, errors.New("easylog/file: Directory must be an absolute path")
	}
	if options.MaxSegmentBytes < 0 {
		return Options{}, errors.New("easylog/file: MaxSegmentBytes must not be negative")
	}
	if options.MaxSegments < 0 {
		return Options{}, errors.New("easylog/file: MaxSegments must not be negative")
	}
	if options.MaxSegmentBytes == 0 {
		options.MaxSegmentBytes = defaultMaxSegmentBytes
	}
	if options.MaxSegments == 0 {
		options.MaxSegments = defaultMaxSegments
	}
	if options.Permissions == 0 {
		options.Permissions = defaultPermissions
	}
	if options.Permissions.Perm() != options.Permissions {
		return Options{}, errors.New("easylog/file: Permissions must contain only permission bits")
	}
	return options, nil
}

func segmentPattern(prefix string) *regexp.Regexp {
	return regexp.MustCompile("^" + regexp.QuoteMeta(prefix) + `_(\d{8})_([0-9]+)\.jsonl$`)
}

func (o *Output) scanSegments(store *levelStore) error {
	entries, err := os.ReadDir(store.directory)
	if err != nil {
		return fmt.Errorf("scan log directory %q: %w", store.directory, err)
	}
	for _, entry := range entries {
		match := store.pattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect segment %q: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		date, err := time.Parse(dateLayout, match[1])
		if err != nil {
			continue
		}
		suffix, err := strconv.ParseUint(match[2], 10, 64)
		if err != nil {
			continue
		}
		current := segment{
			name:   entry.Name(),
			path:   filepath.Join(store.directory, entry.Name()),
			date:   date,
			suffix: suffix,
		}
		store.segments = append(store.segments, current)
	}
	sort.Slice(store.segments, func(i, j int) bool {
		left := store.segments[i]
		right := store.segments[j]
		if !left.date.Equal(right.date) {
			return left.date.Before(right.date)
		}
		if left.suffix != right.suffix {
			return left.suffix < right.suffix
		}
		return left.name < right.name
	})
	return nil
}

func (o *Output) currentDate() time.Time {
	now := o.now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func (o *Output) createSegment(store *levelStore, date time.Time) (*os.File, segment, error) {
	suffix := uint64(0)
	for _, existing := range store.segments {
		if existing.date.Equal(date) && existing.suffix >= suffix {
			suffix = existing.suffix + 1
		}
	}
	for {
		name := fmt.Sprintf("%s_%s_%d.jsonl", store.prefix, date.Format(dateLayout), suffix)
		path := filepath.Join(store.directory, name)
		fileHandle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_APPEND|os.O_WRONLY, o.options.Permissions)
		if errors.Is(err, fs.ErrExist) {
			suffix++
			continue
		}
		if err != nil {
			return nil, segment{}, fmt.Errorf("create segment %q: %w", name, err)
		}
		return fileHandle, segment{
			name:   name,
			path:   path,
			date:   date,
			suffix: suffix,
		}, nil
	}
}

func (o *Output) openSegment(store *levelStore, date time.Time) error {
	var resumeErr error
	if store.resumePending {
		store.resumePending = false
		var resumed bool
		resumed, resumeErr = o.resumeSegment(store, date)
		if resumed {
			return errors.Join(resumeErr, o.cleanupClosedSegments(store))
		}
	}

	active, activeSegment, err := o.createSegment(store, date)
	if err != nil {
		return errors.Join(resumeErr, err)
	}
	store.active = active
	store.activeSegment = activeSegment
	store.activeBytes = 0
	store.segments = append(store.segments, activeSegment)
	return errors.Join(resumeErr, o.cleanupClosedSegments(store))
}

func (o *Output) rotate(store *levelStore, date time.Time) (bool, error) {
	nextFile, nextSegment, err := o.createSegment(store, date)
	if err != nil {
		return false, err
	}
	previous := store.active
	store.active = nextFile
	store.activeSegment = nextSegment
	store.activeBytes = 0
	store.segments = append(store.segments, nextSegment)

	_ = previous.Sync()
	closeErr := previous.Close()
	cleanupErr := o.cleanupClosedSegments(store)
	return true, errors.Join(closeErr, cleanupErr)
}

func (o *Output) resumeSegment(store *levelStore, date time.Time) (bool, error) {
	var candidate *segment
	for index := len(store.segments) - 1; index >= 0; index-- {
		if store.segments[index].date.Equal(date) {
			candidate = &store.segments[index]
			break
		}
	}
	if candidate == nil {
		return false, nil
	}

	active, err := os.OpenFile(candidate.path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return false, fmt.Errorf("resume segment %q: %w", candidate.name, err)
	}
	info, err := active.Stat()
	if err != nil {
		return false, errors.Join(fmt.Errorf("inspect segment %q: %w", candidate.name, err), active.Close())
	}
	if !info.Mode().IsRegular() {
		return false, errors.Join(fmt.Errorf("resume segment %q: not a regular file", candidate.name), active.Close())
	}
	if info.Size() >= o.options.MaxSegmentBytes {
		return false, active.Close()
	}
	if info.Size() > 0 {
		var tail [1]byte
		if _, err := active.ReadAt(tail[:], info.Size()-1); err != nil {
			return false, errors.Join(fmt.Errorf("inspect segment %q ending: %w", candidate.name, err), active.Close())
		}
		if tail[0] != '\n' {
			return false, errors.Join(fmt.Errorf("resume segment %q: incomplete final record", candidate.name), active.Close())
		}
	}

	store.active = active
	store.activeSegment = *candidate
	store.activeBytes = info.Size()
	return true, nil
}

func (o *Output) cleanupClosedSegments(store *levelStore) error {
	for len(store.segments) > o.options.MaxSegments {
		oldest := store.segments[0]
		if oldest.name == store.activeSegment.name {
			return nil
		}
		if err := o.removeSegment(store, oldest); err != nil {
			return err
		}
		store.segments = store.segments[1:]
	}
	return nil
}

func (o *Output) storeForLevel(level slog.Level) *levelStore {
	switch {
	case level < slog.LevelInfo:
		return &o.stores[debugBucket]
	case level < slog.LevelWarn:
		return &o.stores[infoBucket]
	case level < slog.LevelError:
		return &o.stores[warningBucket]
	default:
		return &o.stores[errorBucket]
	}
}

func (o *Output) removeSegment(store *levelStore, candidate segment) error {
	if !store.pattern.MatchString(candidate.name) || filepath.Base(candidate.path) != candidate.name {
		return fmt.Errorf("refuse to remove invalid segment %q", candidate.name)
	}
	info, err := os.Lstat(candidate.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect old segment %q: %w", candidate.name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to remove non-regular segment %q", candidate.name)
	}
	if err := os.Remove(candidate.path); err != nil {
		return fmt.Errorf("remove old segment %q: %w", candidate.name, err)
	}
	return nil
}
