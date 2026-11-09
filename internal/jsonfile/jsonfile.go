// Package jsonfile reads and writes small JSON files. A write goes through a
// temp file in the same folder and a rename, so a reader never sees half a
// file and a failed write leaves the old file whole (#563).
package jsonfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
)

// Load reads the JSON file at path into v. A missing file gives false and no
// error, and v stays as it is. A read error starts with "read <path>: " and a
// parse error with "parse <path>: ".
func Load(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	return true, nil
}

// Write writes data to path. It makes the folder (0755) when it is missing,
// writes a temp file with a unique name in that folder, and renames it to
// path. perm is the mode of the file before the umask. On an error the temp
// file is removed and the old file at path stays as it was.
func Write(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := create(dir, filepath.Base(path), perm)
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// create makes a new file ".<base>.<random>.tmp" in dir. A name that is
// taken gives a new try.
func create(dir, base string, perm fs.FileMode) (*os.File, error) {
	for range 100 {
		name := filepath.Join(dir, "."+base+"."+strconv.FormatUint(rand.Uint64(), 36)+".tmp")
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, fmt.Errorf("temp file in %s: no free name", dir)
}
