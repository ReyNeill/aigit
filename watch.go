package main

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    fsnotify "github.com/fsnotify/fsnotify"
)

// startFsWatch starts a recursive fsnotify watcher rooted at dir.
// It sends a signal on 'events' when any relevant filesystem change occurs.
// Returns a stop function.
func startFsWatch(root string, events chan<- struct{}) (func() error, error) {
    w, err := fsnotify.NewWatcher()
    if err != nil {
        return nil, err
    }

    // helper to add directory if not ignored
    addDir := func(p string) error {
        if shouldIgnorePath(p) {
            return nil
        }
        return w.Add(p)
    }

    // Walk and add initial dirs
    err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() {
            if shouldIgnorePath(p) {
                return filepath.SkipDir
            }
            return addDir(p)
        }
        return nil
    })
    if err != nil {
        _ = w.Close()
        return nil, fmt.Errorf("watch walk: %w", err)
    }

    // Event loop
    go func() {
        defer w.Close()
        emit := func() {
            select { case events <- struct{}{}: default: }
        }
        for {
            select {
            case ev, ok := <-w.Events:
                if !ok { return }
                // ignore .git and hidden/system dirs
                if shouldIgnorePath(ev.Name) {
                    continue
                }
                // If a new directory is created, start watching it
                if ev.Has(fsnotify.Create) {
                    fi, err := os.Stat(ev.Name)
                    if err == nil && fi.IsDir() {
                        _ = addDir(ev.Name)
                    }
                }
                // coalesce bursts by delaying a small amount handled in main loop
                emit()
            case err, ok := <-w.Errors:
                if !ok { return }
                _ = err // ignore; main loop is periodic as safety net
            }
        }
    }()

    stop := func() error {
        // Closing the watcher stops the goroutine
        return nil
    }
    return stop, nil
}

func shouldIgnorePath(p string) bool {
    // Normalize
    p = filepath.ToSlash(p)
    comps := strings.Split(p, "/")
    for _, c := range comps {
        if c == ".git" || c == "node_modules" || c == "vendor" || strings.HasPrefix(c, ".") {
            return true
        }
    }
    return false
}
