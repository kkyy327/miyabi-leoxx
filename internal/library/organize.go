package library

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/movie"
	"github.com/ppxb/miyabi/internal/pan"
)

// sanitizeFolderName removes characters that are invalid in 115 folder names.
func sanitizeFolderName(name string) string {
	// 115 doesn't allow / \ : * ? " < > | in file names
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	sanitized := replacer.Replace(name)
	sanitized = strings.TrimSpace(sanitized)
	if sanitized == "" {
		return "未知"
	}
	return sanitized
}

// OrganizeByActress moves movie files into actress-named folders in 115.
// For each movie with actress info, files are moved to /<media_root>/<actress_name>/.
// Movies without actress info are left in place.
// Database file paths are updated after successful moves.
func (s *Service) OrganizeByActress(ctx context.Context) (int, error) {
	sess, err := s.drive.Open(ctx)
	if err != nil {
		return 0, fmt.Errorf("open drive session: %w", err)
	}

	source := sess.Source()
	rootID := source.Directory.ID
	rootPath := source.Directory.Path // e.g., "/云下载"

	// Query all movies with their actors and files
	movies, err := s.database.Movie.Query().
		WithActors().
		WithFiles().
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query movies: %w", err)
	}

	// Cache of actress directory IDs (name -> 115 dir ID)
	dirCache := make(map[string]string)

	moved := 0
	for _, m := range movies {
		if len(m.Edges.Actors) == 0 {
			continue // No actress info, leave in place
		}
		if len(m.Edges.Files) == 0 {
			continue // No files, nothing to move
		}

		actressName := ""
		if m.Edges.Actors[0].NameZht != nil && *m.Edges.Actors[0].NameZht != "" {
			actressName = sanitizeFolderName(*m.Edges.Actors[0].NameZht)
		} else {
			actressName = sanitizeFolderName(m.Edges.Actors[0].Name)
		}
		targetDirPath := path.Join(rootPath, actressName)

		// Check if files are already in the target directory
		alreadyOrganized := true
		for _, f := range m.Edges.Files {
			fileDir := path.Dir(f.Path)
			if fileDir != targetDirPath {
				alreadyOrganized = false
				break
			}
		}
		if alreadyOrganized {
			continue
		}

		// Get or create the actress directory
		targetDirID, ok := dirCache[actressName]
		if !ok {
			targetDirID, err = s.ensureActressDir(ctx, sess, rootID, actressName)
			if err != nil {
				slog.Warn("failed to create actress directory", "actress", actressName, "error", err)
				continue
			}
			dirCache[actressName] = targetDirID
		}

		// Group files by their current directory
		filesByDir := make(map[string][]*ent.File)
		for _, f := range m.Edges.Files {
			dir := path.Dir(f.Path)
			filesByDir[dir] = append(filesByDir[dir], f)
		}

		// Move files from each source directory
		for srcDirPath, files := range filesByDir {
			if srcDirPath == targetDirPath {
				continue // Already in target
			}
			if err := s.moveMovieFiles(ctx, sess, srcDirPath, targetDirID, targetDirPath, files); err != nil {
				slog.Warn("failed to move movie files", "movie", m.ID, "error", err)
				continue
			}
			moved += len(files)
		}
	}

	return moved, nil
}

// ensureActressDir finds or creates the actress directory under the media root.
func (s *Service) ensureActressDir(ctx context.Context, sess drive.Session, rootID, actressName string) (string, error) {
	// List root directory to check if actress dir already exists
	entries, err := drive.DirectoryEntries(ctx, sess, rootID)
	if err != nil {
		return "", fmt.Errorf("list root: %w", err)
	}
	for _, e := range entries {
		if e.IsDirectory && e.Name == actressName {
			return e.ID, nil
		}
	}
	// Create it
	dirID, err := sess.CreateDirectory(ctx, rootID, actressName)
	if err != nil {
		return "", fmt.Errorf("create directory %s: %w", actressName, err)
	}
	slog.Info("created actress directory", "name", actressName, "id", dirID)
	return dirID, nil
}

// moveMovieFiles moves a movie's files from srcDir to the target actress directory.
// It looks up 115 file IDs by listing the source directory and matching by name,
// then updates the database paths after a successful move.
func (s *Service) moveMovieFiles(ctx context.Context, sess drive.Session, srcDirPath, targetDirID, targetDirPath string, files []*ent.File) error {
	// Find the source directory ID by traversing from root
	// For simplicity, we list the source directory directly if we can resolve its ID.
	// Actually, we need to find the 115 directory ID for srcDirPath.
	//
	// Simpler approach: list the parent and find by name. But we don't have the parent ID easily.
	//
	// Alternative: Use the file's RootID or traverse from the session's root.
	// For now, we'll resolve the directory ID by walking from the media root.

	srcDirID, err := s.resolveDirID(ctx, sess, srcDirPath)
	if err != nil {
		return fmt.Errorf("resolve source dir %s: %w", srcDirPath, err)
	}

	// List source directory to get 115 file IDs by name
	entries, err := drive.DirectoryEntries(ctx, sess, srcDirID)
	if err != nil {
		return fmt.Errorf("list source dir: %w", err)
	}

	// Build name -> ID map
	nameToID := make(map[string]string)
	for _, e := range entries {
		if !e.IsDirectory {
			nameToID[e.Name] = e.ID
		}
	}

	// Collect file IDs to move and their new paths
	var fileIDs []string
	type moveInfo struct {
		file    *ent.File
		newPath string
	}
	var toUpdate []moveInfo

	for _, f := range files {
		fileName := path.Base(f.Path)
		fileID, ok := nameToID[fileName]
		if !ok {
			slog.Warn("file not found in 115 directory", "path", f.Path, "dir", srcDirPath)
			continue
		}
		fileIDs = append(fileIDs, fileID)
		newPath := path.Join(targetDirPath, fileName)
		toUpdate = append(toUpdate, moveInfo{file: f, newPath: newPath})
	}

	if len(fileIDs) == 0 {
		return nil
	}

	// Move via 115 API
	if err := sess.MoveFiles(ctx, targetDirID, fileIDs); err != nil {
		return fmt.Errorf("move files: %w", err)
	}

	// Update database paths
	for _, mi := range toUpdate {
		if err := s.database.File.UpdateOneID(mi.file.ID).SetPath(mi.newPath).Exec(ctx); err != nil {
			slog.Warn("failed to update file path in db", "file", mi.file.ID, "error", err)
			// Don't fail the whole operation; the next scan will reconcile
		}
	}

	slog.Info("moved movie files", "count", len(fileIDs), "from", srcDirPath, "to", targetDirPath)
	return nil
}

// resolveDirID finds the 115 directory ID for a given path by walking from the media root.
func (s *Service) resolveDirID(ctx context.Context, sess drive.Session, dirPath string) (string, error) {
	source := sess.Source()
	rootPath := source.Directory.Path
	rootID := source.Directory.ID

	if dirPath == rootPath {
		return rootID, nil
	}

	// Get relative path from root
	relPath, err := path.Rel(rootPath, dirPath)
	if err != nil {
		return "", err
	}
	parts := strings.Split(relPath, "/")

	currentID := rootID
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		entries, err := drive.DirectoryEntries(ctx, sess, currentID)
		if err != nil {
			return "", err
		}
		found := false
		for _, e := range entries {
			if e.IsDirectory && e.Name == part {
				currentID = e.ID
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("directory not found: %s (part: %s)", dirPath, part)
		}
	}
	return currentID, nil
}

// Ensure pan.File is referenced (for future use)
var _ = pan.File{}
