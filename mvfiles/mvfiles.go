package mvfiles

import (
	"cmp"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unsafe"

	"github.com/carlmjohnson/flagx"
	"github.com/carlmjohnson/versioninfo"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/objc"
)

const AppName = "Scooter"

func CLI(args []string) error {
	var app appEnv
	err := app.ParseArgs(args)
	if err != nil {
		return err
	}
	if err = app.Exec(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	return err
}

func (app *appEnv) ParseArgs(args []string) error {
	fl := flag.NewFlagSet(AppName, flag.ContinueOnError)
	fl.StringVar(&app.dir, "dir", ".", "directory to read")
	fl.BoolVar(&app.excludeDirs, "exclude-dirs", false, "don't move directories")
	fl.BoolVar(&app.dryRun, "dry-run", false, "just output file locations without moving")
	app.Logger = log.New(io.Discard, AppName+" ", log.LstdFlags)
	flagx.BoolFunc(fl, "verbose", "log debug output", func() error {
		app.Logger.SetOutput(os.Stderr)
		return nil
	})
	fl.Usage = func() {
		fmt.Fprintf(fl.Output(), `scooter - %s

Scoot files around by date and kind

Usage:

	scooter [options]

Options:
`, versioninfo.Version)
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		return err
	}
	if err := flagx.ParseEnv(fl, AppName); err != nil {
		return err
	}
	return nil
}

type appEnv struct {
	dir         string
	excludeDirs bool
	dryRun      bool
	*log.Logger
}

func (app *appEnv) Exec() (err error) {
	entries, err := os.ReadDir(app.dir)
	if err != nil {
		return err
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(app.dir, name)
		paths = append(paths, path)
	}
	type pair struct{ old, new string }
	var pairs []pair
	for _, path := range paths {
		newname, err := buildName(path)
		if err != nil {
			return err
		}
		pairs = append(pairs, pair{path, filepath.Join(app.dir, newname)})
	}

	if !app.excludeDirs {
		var dirpaths []string
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") ||
				(len(name) == 4 && strings.HasPrefix(name, "20")) {
				continue
			}
			path := filepath.Join(app.dir, name)
			dirpaths = append(dirpaths, path)
		}
		for _, dirpath := range dirpaths {
			date, err := getDateAdded(dirpath)
			if err != nil {
				return err
			}
			name := filepath.Base(dirpath)
			newname := date.Format("2006/01/") + name
			newpath := filepath.Join(app.dir, newname)
			pairs = append(pairs, pair{dirpath, newpath})
		}
	}

	// Sort by destination
	slices.SortFunc(pairs, func(a, b pair) int {
		return cmp.Compare(a.new, b.new)
	})

	if app.dryRun {
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"old", "new"})
		for _, p := range pairs {
			_ = w.Write([]string{p.old, p.new})
		}
		w.Flush()
		return w.Error()
	}
	for _, p := range pairs {
		dir := filepath.Dir(p.new)
		_ = os.MkdirAll(dir, 0o744)
		if err = os.Rename(p.old, p.new); err != nil {
			return err
		}
	}
	return nil
}

func buildName(path string) (string, error) {
	dateAdded, err := getDateAdded(path)
	if err != nil {
		return "", err
	}
	kind := getKind(path)
	name := filepath.Base(path)
	return fmt.Sprintf("%d/%02d/%s/%s", dateAdded.Year(), dateAdded.Month(), kind, name), nil
}

func getDateAdded(path string) (t time.Time, err error) {
	var (
		ok            bool
		unixTimestamp float64
	)
	s := strings.Clone(path)

	// Was getting random memory corruption,
	// so let's try just throwing in a pool
	objc.WithAutoreleasePool(func() {
		var dateAdded foundation.Date
		var err foundation.Error
		url := foundation.NewURLFileURLWithPath(s)
		ok = url.GetResourceValueForKeyError(
			unsafe.Pointer(&dateAdded),
			foundation.URLAddedToDirectoryDateKey,
			unsafe.Pointer(&err),
		)
		if !ok {
			return
		}
		unixTimestamp = float64(dateAdded.TimeIntervalSince1970())
	})
	if !ok {
		return time.Time{}, fmt.Errorf("could not read %q", path)
	}

	seconds := math.Floor(unixTimestamp)
	nanoseconds := (unixTimestamp - seconds) * 1e9

	return time.Unix(int64(seconds), int64(nanoseconds)), nil
}

func getKind(name string) string {
	ext := path.Ext(name)
	ext = strings.TrimPrefix(ext, ".")
	ext = strings.ToLower(ext)
	for _, s := range []string{
		"archive: bz dmg gz tar tbz2 zip",
		"audio: aac m4a mp3 wav",
		"data: csv json xls xlsx",
		"doc: doc docx pages pdf rtf rtfd txt",
		"book: epub",
		"image: avif bmp gif heic jpg jpeg  png svg tif webp",
		"video: avi mp4 mpeg",
		"web: css html ico js sass",
	} {
		kind, fields, _ := strings.Cut(s, ":")
		exts := strings.Fields(fields)
		if slices.Contains(exts, ext) {
			return kind
		}
	}
	return "misc"
}
