package util

import (
	"fmt"
	"os"
)

// OwnershipDisabled is the uid/gid value meaning "leave ownership alone".
// Zero is a legitimate id (root), so it cannot be used as the sentinel.
const OwnershipDisabled = -1

// FileOwnership records the uid and gid that files written by this process
// should carry.
//
// The container runs as root, so anything it writes into a media library lands
// as root:root while the library itself is owned by a real user. That makes the
// files unmanageable from outside the container — the reason the output
// directory could not be cleaned up without elevation.
//
// This sets ownership on files after they are written rather than dropping the
// process's privileges. It matches what PUID/PGID achieve for the user (files
// owned correctly) without the re-exec that a full privilege drop requires; the
// stronger option is to run the whole container as a non-root user.
type FileOwnership struct {
	UID int
	GID int
}

// NoOwnership leaves file ownership untouched.
func NoOwnership() FileOwnership {
	return FileOwnership{UID: OwnershipDisabled, GID: OwnershipDisabled}
}

// Enabled reports whether either id is set.
func (o FileOwnership) Enabled() bool {
	return o.UID != OwnershipDisabled || o.GID != OwnershipDisabled
}

func (o FileOwnership) String() string {
	if !o.Enabled() {
		return "unchanged"
	}
	return fmt.Sprintf("%d:%d", o.UID, o.GID)
}

// Apply sets ownership on path. It is a no-op when ownership is not configured.
//
// Errors are returned rather than ignored, but callers should generally treat
// them as non-fatal: a transcode that succeeded and then failed to chown is
// still a usable file, and on some filesystems (notably NFS with root_squash)
// chown is simply not permitted.
func (o FileOwnership) Apply(path string) error {
	if !o.Enabled() {
		return nil
	}
	if err := os.Chown(path, o.UID, o.GID); err != nil {
		return fmt.Errorf("setting ownership %s on %s: %w", o, path, err)
	}
	return nil
}
