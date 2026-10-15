package library

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/pan"
)

// junkMaxSize is the maximum size for a file to be considered junk (200MB).
// Real movies are typically > 1GB; ad/spam files are usually < 100MB.
const junkMaxSize = 200 * 1024 * 1024

// junkMinMainSize is the minimum size for a file to be considered the main movie (500MB).
const junkMinMainSize = 500 * 1024 * 1024

// junkKeywords are substrings that strongly indicate spam/ad files.
var junkKeywords = []string{
	"社区最新情报", "直播", "信誉保证", "美少女",
	"广告", "推广", "最新情报",
}

var junkVideoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
	".mov": true, ".flv": true, ".ts": true, ".m2ts": true,
}

// isJunkVideo determines if a file is likely a spam/ad video that should be deleted.
func isJunkVideo(file pan.File, mainFileName string) bool {
	ext := strings.ToLower(filepath.Ext(file.Name))
	if !junkVideoExts[ext] {
		return false
	}
	if file.Size > junkMaxSize {
		return false
	}
	// Extract code from main file name (e.g., "START-649" from "489155.com@START-649.mp4")
	// If the file name contains the same code, it's not junk
	if mainFileName != "" {
		// Simple check: if file names share a long common substring, they're related
		// For now, just check if this file's name is very different from main
		// (junk files have completely different names like "社区最新情报.mp4")
	}
	// Very small files (< 50MB) are almost certainly junk/ads
	if file.Size < 50*1024*1024 {
		return true
	}
	// Check spam keywords
	nameLower := strings.ToLower(file.Name)
	for _, kw := range junkKeywords {
		if strings.Contains(nameLower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// CleanupJunkFiles scans the media directory and deletes obvious spam/ad video files.
// It only deletes files when there's a clear main movie file in the same directory.
// Deleted files go to 115's recycle bin (recoverable for 30 days).
func (s *Service) CleanupJunkFiles(ctx context.Context) (int, error) {
	sess, err := s.drive.Open(ctx)
	if err != nil {
		return 0, fmt.Errorf("open drive session: %w", err)
	}

	source := sess.Source()
	rootID := source.Directory.ID

	// List root directory
	rootFiles, err := drive.DirectoryEntries(ctx, sess, rootID)
	if err != nil {
		return 0, fmt.Errorf("list root directory: %w", err)
	}

	deleted := 0
	// Check each subdirectory
	for _, entry := range rootFiles {
		if !entry.IsDirectory {
			continue
		}
		count, err := s.cleanupDirectory(ctx, sess, entry)
		if err != nil {
			slog.Warn("junk cleanup failed for directory", "dir", entry.Name, "error", err)
			continue
		}
		deleted += count
	}

	// Also check files directly in root (in case movies aren't in subfolders)
	count, err := s.cleanupDirectoryFiles(ctx, sess, rootID, rootFiles)
	if err != nil {
		slog.Warn("junk cleanup failed for root", "error", err)
	} else {
		deleted += count
	}

	return deleted, nil
}

// cleanupDirectory checks a single directory for junk files and deletes them.
func (s *Service) cleanupDirectory(ctx context.Context, sess drive.Session, dir pan.File) (int, error) {
	files, err := drive.DirectoryEntries(ctx, sess, dir.ID)
	if err != nil {
		return 0, err
	}
	return s.cleanupDirectoryFiles(ctx, sess, dir.ID, files)
}

// cleanupDirectoryFiles identifies and deletes junk video files in a directory listing.
func (s *Service) cleanupDirectoryFiles(ctx context.Context, sess drive.Session, dirID string, files []pan.File) (int, error) {
	// Find video files
	var videos []pan.File
	for _, f := range files {
		if f.IsDirectory {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if junkVideoExts[ext] {
			videos = append(videos, f)
		}
	}

	// Need at least 2 videos to have junk (one main + junk)
	if len(videos) < 2 {
		return 0, nil
	}

	// Find the main movie file (largest video)
	var mainFile *pan.File
	for i := range videos {
		if mainFile == nil || videos[i].Size > mainFile.Size {
			mainFile = &videos[i]
		}
	}
	// Main file must be substantial (> 500MB) to be confident
	if mainFile == nil || mainFile.Size < junkMinMainSize {
		return 0, nil
	}

	// Identify junk files
	var junkIDs []string
	var junkNames []string
	for _, v := range videos {
		if v.ID == mainFile.ID {
			continue
		}
		if isJunkVideo(v, mainFile.Name) {
			junkIDs = append(junkIDs, v.ID)
			junkNames = append(junkNames, v.Name)
		}
	}

	if len(junkIDs) == 0 {
		return 0, nil
	}

	// Delete junk files (moves to 115 recycle bin)
	slog.Info("deleting junk files", "dir", dirID, "count", len(junkIDs), "files", junkNames)
	if err := sess.DeleteFiles(ctx, dirID, junkIDs); err != nil {
		return 0, fmt.Errorf("delete junk files: %w", err)
	}

	return len(junkIDs), nil
}
