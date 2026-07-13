package fsops

import "os"

// ReadFileAllowMissing reads the file at path, distinguishing a legitimately
// absent file from a real read error. A missing file (os.IsNotExist) returns
// (nil, false, nil); any other error (permission denied, I/O failure, ...) is
// surfaced as (nil, false, err) instead of being silently conflated with
// absence; a successful read returns (data, true, nil).
//
// This is the shared primitive for the "swallow-and-degrade" remediation:
// callers that used to do `if err != nil { return <empty> }` after
// os.ReadFile can switch to this helper and get a correct three-way split
// (absent / real error / present) without hand-rolling the os.IsNotExist
// check at every call site.
func ReadFileAllowMissing(path string) (data []byte, found bool, err error) {
	data, err = os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// ReadDirAllowMissing reads the directory at path, applying the same
// absent/real-error/present three-way split as ReadFileAllowMissing.
func ReadDirAllowMissing(path string) (entries []os.DirEntry, found bool, err error) {
	entries, err = os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return entries, true, nil
}

// StatAllowMissing stats path, applying the same absent/real-error/present
// three-way split as ReadFileAllowMissing.
func StatAllowMissing(path string) (info os.FileInfo, found bool, err error) {
	info, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return info, true, nil
}
