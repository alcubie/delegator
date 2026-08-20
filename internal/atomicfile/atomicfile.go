// Package atomicfile writes a file in one step. It is the way that delegator
// makes each file that holds data. Section 7 of docs/TECHNICAL_DESIGN.md says
// that files are the data, so a file that is half written is a loss of data.
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Perm is the permission of each file that delegator writes. Section 11 says
// that the data can be private, so only its person can read it.
const Perm fs.FileMode = 0o600

// Write puts data in a temporary file in the same directory, and then gives that
// file the name of the target. A rename in one directory cannot occur in part,
// so a write that stops leaves the old file. The temporary file is in the same
// directory because a rename between two file systems is not possible.
func Write(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// This removes the temporary file after an error. After a rename that
	// operates, the name is not in use and the removal does nothing.
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	// Sync puts the data on the disk before the rename. Without it, a loss of
	// power can leave a file that has the new name and no contents.
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
