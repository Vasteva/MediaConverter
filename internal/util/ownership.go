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
// As of #37 the container's entrypoint drops root and runs this process as
// PUID:PGID directly, so files it writes are correctly owned from the
// moment they're created — Apply below is a same-owner no-op in that,
// now-common, case. It still exists for two reasons: the entrypoint falls
// back to a fixed non-root uid when PUID/PGID aren't set, so a job with a
// different explicit ownership requirement still has a path to it; and it's
// the same call the pre-#37 root-container fallback used, before anything
// this process wrote could be trusted to already carry the right owner.
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
