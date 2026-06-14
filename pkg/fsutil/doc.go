// Package fsutil provides grew's low-level filesystem helpers: advisory file
// locking, containment-checked copying, atomic writes, mode sanitization, and
// directory size/cleanup utilities.
//
// The package's defining concern is safety. Copies and removals are validated
// through pkg/safepath so they cannot escape their destination root, archive
// modes are scrubbed of dangerous bits before they touch disk, and writes are
// atomic so a crash never leaves a half-written file.
//
// # Locking
//
// [Lock], [TryLock], and [Unlock] wrap flock(2) advisory locks on an open
// file — Lock blocks, TryLock returns immediately (syscall.EWOULDBLOCK if the
// lock is held). These back grew's global install lock.
//
// # Copying
//
// [CopyTree] recursively copies a directory tree, preserving symlinks but
// refusing (and logging) any link or entry that would resolve outside the
// destination root. [CopyFileWithinRoot] copies a single file under a root it
// enforces both lexically and after resolving symlinks — covering the case
// where an intermediate directory is itself a symlink out of the tree — and
// refuses to overwrite a non-regular destination. [CopyFile] is the convenience
// wrapper that skips the cross-tree root check.
//
// # Atomic writes and modes
//
// [WriteFileAtomic] writes to a temp file in the destination directory, chmods
// it, then renames over the target, cleaning up on any failure. [SanitizeMode]
// applies grew's policy to archive-extracted modes: setuid/setgid/sticky and
// world-write are stripped, and a sensible default is supplied when the mode
// is zero.
//
// # Sizes and cleanup
//
// [DiskUsage], [DirSize], and [EntrySize] report on-disk usage; [FormatSize]
// renders a byte count human-readably. [PruneEmptyDirs] removes empty
// directories bottom-up, and [RemoveIfWithinAllowed] deletes a file only when
// it lives inside the temp or cache directory, refusing anything outside.
package fsutil
