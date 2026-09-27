// This file holds the stat-keyed manifest cache. It exists because rescanning
// an unchanged Mods folder is the normal case: the product re-reads the tree on
// every refresh, and re-parsing every manifest of a tree that hasn't moved is
// pure waste. The key is the manifest file's path relative to the scanned root
// plus the size and modification time the filesystem reports — the facts any
// fs.FS hands out for free, and the ones an author's edit disturbs. Content
// hashes are deliberately never part of a key: hashing is the expensive part,
// so it happens only when a caller asks for it (Hash), and even then only once
// per observed stat. Everything lives in memory and dies with the process, so a
// restart always re-reads the tree from scratch.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sync"
)

// Cache is an in-memory, stat-keyed cache of parsed manifests for one scanned
// Mods root. Nothing persists across restarts; it is safe for concurrent use.
//
// A stat change invalidates exactly the entry it names: the key carries the
// size and modification time, so a stale parse can never be served under a new
// stat, and storing a new version drops the old one for that path. Only
// successful reads are ever stored — a read or stat failure leaves no entry, so
// a transient failure is retried on the next scan instead of being remembered
// as a result. Folders with no manifest at all (ignored, empty, XNB,
// unreadable, manifest-less) are never represented here and are re-derived by
// every scan.
//
// Units served by the cache are shared between scans and records: callers MUST
// treat them as read-only. Construct one with NewCache; the zero Cache is not
// usable.
type Cache struct {
	mu      sync.Mutex
	entries map[cacheKey]cacheEntry
	byPath  map[string]cacheKey // newest key per manifest path, so a changed file drops its stale entry
}

// cacheKey identifies one observed version of one manifest file: the path
// relative to the scanned root plus the size and modification time a stat
// reported. A file whose stat moved makes a different key, so it simply misses.
type cacheKey struct {
	path    string
	size    int64
	modTime int64 // nanoseconds since the epoch, as stat reported them
}

// cacheEntry is what one stat key serves. Both halves fill in on demand: a scan
// fills unit, a Hash call fills hash.
type cacheEntry struct {
	unit *Unit
	hash string
}

// NewCache returns an empty cache. Keys are paths relative to the scanned root,
// so give each root its own cache: two roots sharing one could collide.
func NewCache() *Cache {
	return &Cache{entries: make(map[cacheKey]cacheEntry), byPath: make(map[string]cacheKey)}
}

// Hash returns the hex SHA-256 of a file's contents, computed on demand and
// reused while that file's stat (path, size, mtime) is unchanged.
//
// The hash is never part of a cache key and no scan computes one: it is the
// escape hatch for the one thing a stat-keyed cache cannot see, a rewrite that
// preserves size and modification time, and it costs a read only the first time
// a given stat is hashed. A file that can't be stat'd or read returns its error
// and stores nothing, so the next call retries rather than reading a failure as
// a result.
func (c *Cache) Hash(fsys fs.FS, name string) (string, error) {
	before, err := fs.Stat(fsys, name)
	if err == nil {
		if sum, ok := c.hashed(statKey(name, before)); ok {
			return sum, nil
		}
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	if before != nil {
		c.remember(fsys, name, before, func(entry *cacheEntry) { entry.hash = hexSum })
	}
	return hexSum, nil
}

// unit returns the parsed Unit for the manifest at name, reading and parsing it
// only when no stored entry matches the file's current stat, so a cache hit
// never touches the file's bytes. The error is the read's own error, unchanged
// from a cache-less scan; a stat failure is not fatal because the read that
// follows still decides.
func (c *Cache) unit(fsys fs.FS, name string) (*Unit, error) {
	before, err := fs.Stat(fsys, name)
	if err == nil {
		if unit, ok := c.parsed(statKey(name, before)); ok {
			return unit, nil
		}
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	unit := ParseUnit(data)
	if before != nil {
		c.remember(fsys, name, before, func(entry *cacheEntry) { entry.unit = unit })
	}
	return unit, nil
}

// hashed serves a stored hash, reporting false when this stat was never hashed.
func (c *Cache) hashed(key cacheKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	return entry.hash, ok && entry.hash != ""
}

// parsed serves a stored parse, reporting false when this stat was never parsed.
// The unit is the same pointer earlier callers received: it is shared, read-only.
func (c *Cache) parsed(key cacheKey) (*Unit, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	return entry.unit, ok && entry.unit != nil
}

// remember stores what a read produced under the stat observed before that
// read, and only if the file's stat still matches now — the stat, read, stat
// guard. A file that changed while it was being read, or that can no longer be
// stat'd, is never stored: keeping the bytes that happened to arrive under a
// stat they no longer belong to is exactly the stale read this cache exists to
// prevent. Storing also drops any earlier entry for the same path, so a changed
// file invalidates its old entry instead of leaving it unreachable.
func (c *Cache) remember(fsys fs.FS, name string, before fs.FileInfo, fill func(*cacheEntry)) {
	after, err := fs.Stat(fsys, name)
	if err != nil || !sameStat(before, after) {
		return
	}
	key := statKey(name, before)
	c.mu.Lock()
	defer c.mu.Unlock()
	if stale, ok := c.byPath[key.path]; ok && stale != key {
		delete(c.entries, stale)
	}
	c.byPath[key.path] = key
	entry := c.entries[key]
	fill(&entry)
	c.entries[key] = entry
}

// statKey projects one stat observation onto a cache key: the file's path
// relative to the scanned root, its size, and its modification time. A rewrite
// that preserves both size and mtime is invisible here by design, which is what
// Hash is for.
func statKey(name string, info fs.FileInfo) cacheKey {
	return cacheKey{path: name, size: info.Size(), modTime: info.ModTime().UnixNano()}
}

// sameStat reports whether two observations describe the same version of a file.
func sameStat(a, b fs.FileInfo) bool {
	return a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
