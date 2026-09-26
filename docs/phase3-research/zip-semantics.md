# ZIP semantics in Go: `archive/zip`, `os`, `path/filepath`

Research for ticket #45 (child of the Phase 3 map). Scope: what Go 1.25's
`archive/zip` actually reports per entry for hostile and unusual ZIPs, where it
normalizes or rejects, what `os` / `path/filepath` do with the resulting names
on Windows, macOS and Linux, how decompression-bomb budgets can be enforced
while streaming, and what limits comparable tools document.

All Go line numbers are from the toolchain this repo pins (`go.mod`:
`go 1.25.0`, `toolchain go1.25.14`); paths are relative to `src/` of
`golang.org/toolchain@v0.0.1-go1.25.14`. Tags used below:

- `[VERIFIED]` — read directly in Go source or upstream docs.
- `[VERIFIED: Go test]` — asserted by the Go package's own tests.
- `[INFERENCE]` — derived from the cited source, not stated by it.

## 1. What `zip.Reader` gives you

`zip.NewReader` / `zip.OpenReader` parse the **central directory eagerly** and
return a fully populated `Reader.File []*File` before any entry is read
(`archive/zip/reader.go:119` `Reader.init`, called from `:78` `OpenReader` and
`:107` `NewReader`). `OpenReader` is `os.Open` + `Stat().Size()` + `NewReader`
(`reader.go:78`). There is no incremental/streaming parse and no API to limit
how much header data is read.

For every entry Go stores the central-directory record as-is:

- `f.Name = string(d[:filenameLen])` (`reader.go:357` `readDirectoryHeader`) —
  **raw bytes**, no decoding, no normalization, no validation. `filenameLen` is
  a `uint16`, so a name is at most 65535 bytes by construction.
- NUL bytes, invalid UTF-8, backslashes, drive letters, `..`, trailing dots and
  spaces, reserved device names all survive verbatim. There is no filter
  anywhere in the read path except the opt-in check in §2.
- The **local** file header is only used to locate the body:
  `findBodyOffset` (`reader.go:339`) reads the local signature plus the local
  `filenameLen`/`extraLen`. Sizes, names, CRC and attributes always come from
  the central directory, so local/central disagreement is ignored.
- `FileHeader.NonUTF8` is *detected* but never acted on: `detectUTF8`
  (`archive/zip/writer.go:231`) + the decision block in `readDirectoryHeader`
  set `NonUTF8 = true` when name/comment are definitely not UTF-8; **CP437 (or
  any other legacy encoding) is not transcoded** — the bytes pass through.
  If the name is ASCII-ish or valid UTF-8 the flag is trusted
  (`f.NonUTF8 = f.Flags&0x800 == 0`).
- `FileHeader.Mode()` (`struct.go:313`) maps unix mode bits only when
  `CreatorVersion>>8` is `3` (UNIX) or `19` (macOS X); MS-DOS attributes only
  when it is `0` (FAT), `11` (NTFS) or `14` (VFAT); **any other creator byte
  yields `Mode() == 0`** (no type bits, no permissions). Independently,
  `Mode()` adds `fs.ModeDir` when the name ends with `/`.
- `File.Open()` (`reader.go:218`) is the only place directories are detected:
  `strings.HasSuffix(f.Name, "/")`, *not* `Mode().IsDir()`. See row 16 in §4.
- `File.OpenRaw()` (`reader.go:261`) returns the raw (possibly compressed,
  possibly encrypted) bytes with **no decompression, no size check and no CRC
  check**. Do not use it for extraction.
- Entry count is not limited: `Reader.init` reads directory headers until one
  fails and then only compares the count modulo 65536
  (`if uint16(len(r.File)) != uint16(end.directoryRecords)`), so >65535 entries
  work (`zip_test.go:24` `TestOver65kFiles` writes 65578).

Error surface from `archive/zip` (all exported, `reader.go:28-33`):
`ErrFormat`, `ErrAlgorithm`, `ErrChecksum`, `ErrInsecurePath`, plus
`io.ErrUnexpectedEOF` leaking through `File.Open`/reads, and `fs.ErrInvalid` /
`fs.ErrNotExist` from the `fs.FS` view.

## 2. The only built-in name filter: `ErrInsecurePath` (opt-in)

`Reader.init` ends with (`reader.go:171-180`):

```go
if zipinsecurepath.Value() == "0" {
    for _, f := range r.File {
        if f.Name == "" { continue } // Zip permits an empty file name field.
        // The zip specification states that names must use forward slashes,
        // so consider any backslashes in the name insecure.
        if !filepath.IsLocal(f.Name) || strings.Contains(f.Name, `\`) {
            zipinsecurepath.IncNonDefault()
            return ErrInsecurePath
        }
    }
}
```

Decision-relevant properties:

- **Opt-in only.** `internal/godebugs/table.go:81` lists
  `{Name: "zipinsecurepath", Package: "archive/zip"}` with no `Changed:` field,
  i.e. the default is still “off” in Go 1.25; the Go 1.20 release notes say
  “A future version of Go may disable insecure paths by default.”
  The gate is `GODEBUG=zipinsecurepath=0`.
- **Whole archive, no attribution.** It returns one sentinel error; the caller
  must re-scan `r.File` to name the offending entry.
- **The reader is still returned and still usable** — `OpenReader`/`NewReader`
  return `(r, ErrInsecurePath)` and only `Close` the file when the error is
  something else (`reader.go:88-91`, `:110-114`). Code that does
  `if err != nil { return }` gets whole-archive rejection for free; code that
  ignores the error gets the hostile names. `TestCVE202127919` /
  `TestOpenReaderInsecurePath` assert exactly this: `r.File[0].Name ==
  "../test.txt"`, and `r.File[0].Open()` succeeds.
- **OS-dependent semantics**, because `filepath.IsLocal` is: on Windows it also
  rejects reserved device names, any `:` and `..`-escaping; on Unix it only
  rejects rooted/`..`/empty. The extra backslash test is OS-independent.
- Writers are not protected: `TestInsecurePaths` writes `../foo`, `/foo`,
  `a/b/../../../c`, `a\b` with `zip.Writer.Create` and gets no error —
  the documented “must not start with a drive letter … only forward slashes”
  rule in `writer.go:212-218` is **not enforced**; the only name check in the
  writer is `len(name) > 65535 ⇒ errLongName` (`writer.go:389`).

## 3. The `fs.FS` view silently rewrites names

`zip.Reader` implements `fs.FS` (`reader.go:897` `Open`, `:985`
`openDir.ReadDir`). Before anything else it builds `fileList` with
`toValidName` (`reader.go:793`):

```go
name = strings.ReplaceAll(name, `\`, `/`)
p := path.Clean(name)
p = strings.TrimPrefix(p, "/")
for strings.HasPrefix(p, "../") { p = p[len("../"):] }
return p
```

Consequences per hostile class (entries whose sanitized name is `""` — i.e.
names made only of separators such as `/`, `//`, `\`, `////` — are **dropped
from the fs view entirely**):

| Raw `File.Name` | fs.FS view | Reachable via `Reader.Open` |
|---|---|---|
| `../x`, `a/../../x` | `x` | yes (`TestCVE202127919`) |
| `/foo`, `//foo` | `foo` | yes |
| `a\b.txt`, `a\b/c` | `a/b.txt`, `a/b/c` | yes |
| `\\srv\share\x` | `srv/share/x` | yes |
| `\\?\C:\x` | `?/C:/x` | yes |
| `C:/x` | `C:/x` (unchanged) | **yes** — drive paths are not escaped |
| `C:x` | `C:x` (unchanged) | **yes** |
| `\`, `/`, `//` | `""` | no; dropped |
| `.` | `.` | the root itself; a *file* named `.` is unreachable and makes `ReadDir` fail |
| `..` | `..` | no (`fs.ValidPath` rejects); `ReadDir` errors |

Other properties of the fs view:

- `Open` gates the *lookup* with `fs.ValidPath` (`reader.go:901`), which rejects
  `.`/`..` elements, empty elements and leading/trailing slashes but **accepts**
  `\` and `:` (see §7) — and since the fs view never contains a backslash, such
  lookups simply miss.
- Directory entries are synthesized from name prefixes: `a/b/c` yields `a`,
  `a/b`, `a/b/c` in `ReadDir` (`initFileList`, `reader.go:806`; `TestFSWalk`).
- **Duplicates are flagged, not removed.** The first occurrence of a name gets
  `isDup = true`; `Open` ignores the flag and returns the first entry, while
  `ReadDir`/`stat` return `"<name>: duplicate entries in zip file"`
  (`reader.go:761`). `testdata/dupdir.zip` is the fixture; `TestFSWalk` expects
  an error. A file whose name is also a directory prefix is flagged the same
  way.
- The view is byte-exact and case-sensitive; `A.txt` and `a.txt` are distinct.
- Names sanitized to `.` or `..` stay in the list and make `ReadDir` fail with
  `invalid file name: …` (`reader.go:987-995`), so `fs.WalkDir` over such an
  archive errors out (`TestFSWalkBadFile`).
- Because rewriting is silent, the fs view **cannot** be used for policy
  checks: it neither reports nor rejects the hostile spelling. Policy must run
  over `r.File[i].Name`.

## 4. Per-entry matrix (the ticket's list)

“Host risk” = what happens if a naive extractor joins the name onto a
destination root and calls `os.Create`/`os.MkdirAll`.

| # | Central-directory entry | `File.Name` (raw) | `Mode()` / flags | `Open()` result | fs.FS view | Host risk |
|---|---|---|---|---|---|---|
| 1 | `../x`, `a/../../x` | verbatim | per creator | data | `x` | escapes root via `filepath.Join` (Join cleans, does not confine) |
| 2 | mixed `a\b/c` | verbatim | per creator | data | `a/b/c` | Windows: silent extra level; Unix: one filename with `\` |
| 3 | `/abs/x` | verbatim | per creator | data | `abs/x` | Unix absolute; Windows rooted on current drive |
| 4 | `C:/x`, `C:\x` | verbatim | per creator | data | `C:/x` | Windows drive-absolute |
| 5 | `C:x` (drive-relative) | verbatim | per creator | data | `C:x` | Windows: invalid name, or literally `C:x` under `\\?\` |
| 6 | `\\srv\share\x` (UNC) | verbatim | per creator | data | `srv/share/x` | Windows network path |
| 7 | `\\?\C:\x`, `\\.\PHYSICALDRIVE0` | verbatim | per creator | data | `?/C:/x` | Windows device paths |
| 8 | `CON`, `NUL`, `COM1`, `CONIN$`, `NUL ` | verbatim | per creator | data | same | Windows opens a device; `IsLocal` rejects on Windows only |
| 9 | `foo.`, `foo ` (trailing dot/space) | verbatim | per creator | data | same | Windows Win32 strips ⇒ collides with `foo`; `IsLocal` does **not** reject |
| 10 | `a.txt` + `A.TXT` | both verbatim | per creator | both data | distinct | case-insensitive hosts: second write wins |
| 11 | `a.txt` twice | both verbatim | per creator | both data | first flagged dup | last writer wins; fs view errors on `ReadDir` |
| 12 | CP437 / invalid UTF-8 name | verbatim bytes, `NonUTF8=true` | per creator | data | same bytes | Windows: invalid bytes → U+FFFD (`syscall.encodeWTF16`), collisions possible |
| 13 | 500-deep `a/…/x` | verbatim | per creator | data | same | Windows 248/260 or `\\?\`; Linux `PATH_MAX` 4096 |
| 14 | 65535-byte name | verbatim | per creator | data | same | host `NAME_MAX` 255 (Linux) / 255 UTF-16 units (NTFS) |
| 15 | unix symlink (`S_IFLNK`) | verbatim | `ModeSymlink\|perm` | **target string as data** | same | symlink only if the extractor creates it deliberately |
| 16 | unix/MS-DOS dir bit, no trailing `/` | verbatim | `ModeDir` set | **data, not empty** | treated as a file | mode/dir disagreement to normalize |
| 17 | name ends `/`, declared size 0 | verbatim | `ModeDir` | `io.EOF` reader | directory | — |
| 18 | name ends `/`, declared uncompressed size > 0 | verbatim | `ModeDir` | `dirReader{ErrFormat}`; first `Read` errors | directory | reject as malformed |
| 19 | MS-DOS-attrs-only entry (creator 0/11/14) | verbatim | `0666` or `ModeDir\|0777`, `-w` if read-only | data | same | no exec bit; read-only bit lost on unix hosts |
| 20 | creator byte not in {0,3,11,14,19} | verbatim | `0` | data | same | `Mode().IsDir()` false; `Mode().Perm()` = 0 |
| 21 | encrypted (GPB bit 0) | verbatim | nothing special | Deflate: flate error; Store: ciphertext + `ErrChecksum` | same | must be detected via `f.Flags&0x1 != 0` |
| 22 | zip64 entry | verbatim | per creator | data | same | declared sizes up to 2⁶⁴-1 |
| 23 | data descriptor (GPB bit 3) | verbatim | nothing special | data, CRC re-checked against the descriptor | same | descriptor sizes ignored |
| 24 | unknown compression method | verbatim | per creator | `ErrAlgorithm` | same | reject per entry |
| 25 | empty name (`""`) | `""` | per creator | data | dropped | must reject; `init` skips it in the insecure check |
| 26 | name `.` or `..` | verbatim | per creator | data | kept, then `ReadDir` errors | reject |
| 27 | name `/`, `//`, `\` | verbatim | per creator | data | dropped | reject |

Notes on the rows that are easy to get wrong:

- **Row 15 (symlink).** Go never resolves, rejects or creates anything: a
  symlink entry is a zero-byte (or target-string) file whose `Mode()` has
  `fs.ModeSymlink`, and it only has that bit when the creator byte is 3 or 19
  and the unix mode bits contain `0xA000` (`struct.go:391` `unixModeToFileMode`).
  A symlink created by a Windows tool (creator 0/11) is *not* visible as a
  symlink — it is an ordinary file. `zip.Writer.SetMode` (`struct.go:327`)
  always writes unix bits + MS-DOS attributes.
- **Row 16 (MS-DOS dir bit without `/`).** `Mode()` sets `ModeDir` from the
  attribute byte, but `File.Open()` only looks at the trailing slash, so
  `fs.Stat(f).IsDir()` and `f.Open()` disagree. Any extractor must decide which
  one wins; the fs.FS view uses the trailing slash only.
- **Row 18 (directory with declared data).** `dirReader` returns `ErrFormat`
  on the first `Read` when `UncompressedSize64 != 0`; a *compressed* directory
  with `UncompressedSize64 == 0` is deliberately tolerated (Java `jar`
  produces those — `TestCompressedDirectory`).
- **Rows 21 (encryption).** There is no `IsEncrypted` in `archive/zip` and
  `Flags&0x1` is never consulted (`archive/zip/*.go` has no reference to the
  encryption bit). The ciphertext is fed to the decompressor, so the observable
  result is a `flate` error, a `ErrChecksum`, or — if the CRC happens to match
  what the attacker declared — garbage bytes. Detecting password-protected
  entries requires testing `f.Flags&0x1 != 0` yourself. `[VERIFIED]` for
  “nothing checks it”; the concrete failure mode per method is `[INFERENCE]`.
- **Row 22 (zip64).** The zip64 extra (`0x0001`) replaces the `0xFFFFFFFF`
  placeholders; `f.zip64` is exposed on `File`. If the extra field is *absent*
  and a size is `0xFFFFFFFF`, Go accepts 4294967295 as the literal size — the
  comment in `readDirectoryHeader` says this “keeps archive/zip working with
  42.zip”. A hostile zip64 size can be up to 2⁶⁴-1;
  `int64(f.CompressedSize64)` in `File.Open` would then go negative, giving the
  section reader a negative limit and an immediate `io.EOF` →
  `io.ErrUnexpectedEOF` from the checksum reader. `[INFERENCE]` on the
  negative-length path; the 64-bit attacker control is `[VERIFIED]`.
- **Row 23 (data descriptors).** `hasDataDescriptor()` is `Flags&0x8 != 0`
  (`struct.go:345`). The descriptor is read at
  `headerOffset+bodyOffset+CompressedSize64` and only its CRC is compared
  (`reader.go:529` `readDataDescriptor`; both signature/no-signature forms are
  accepted, and the descriptor's size fields are documented in-source as
  ignorable). Because sizes come from the central directory, an archive with
  wrong descriptor sizes still extracts correctly.
- **Row 12 (CP437).** The bytes are never decoded, so a CP437 `ä` shows up as a
  single 0x84 byte. On Windows, `syscall.UTF16FromString` encodes invalid
  UTF-8 bytes as `utf8.RuneError` (U+FFFD) — `syscall/wtf8_windows.go:44`
  `encodeWTF16` — so such names are *mangled* at creation time, not rejected.
  On Linux they are stored as the raw bytes they are. `[VERIFIED]` source
  reading; the U+FFFD outcome is `[INFERENCE]` from `utf16.AppendRune(RuneError)`.

## 5. Decompression-bomb mechanics and where budgets can be enforced

### 5.1 What the header can lie about

| Field | How Go uses it | Consequence for budgets |
|---|---|---|
| `UncompressedSize64` | hard cap: `checksumReader.Read` returns `ErrFormat` as soon as `nread > UncompressedSize64`; at EOF a mismatch returns `io.ErrUnexpectedEOF` (`reader.go:295-333`) | declared size is an *upper bound on bytes you can obtain from `File.Open()`* (`TestUnderSize`); can still be 2⁶⁴-1, so it is not a bound |
| `CompressedSize64` | section reader length for the body | attacker-controlled; also the only cheap “bytes charged against the archive” metric |
| `CRC32` | checked **only** when the reader reaches EOF (or when a data descriptor is present, then against the descriptor) | partial reads verify nothing |
| `Flags` bit 3 | descriptor present ⇒ CRC compared at EOF | — |
| `Method` | `ErrAlgorithm` for anything but Store/Deflate (plus globally registered replacements, `RegisterDecompressor`) | reject unknown methods early |
| local header sizes | **ignored** | local/central disagreement is not a detection signal |
| EOCD counts | used for the modulo-65536 comparison and the bounded preallocation only | a hostile EOCD count cannot force a huge allocation (`TestCVE202133196`, `TestCVE202139293`) |

Parse-time memory is bounded by the archive size: each central-directory
record costs one `*File` plus copies of name/extra/comment (`reader.go:357`),
and the preallocation is guarded by
`if end.directorySize < uint64(size) && (uint64(size)-end.directorySize)/30 >= end.directoryRecords`
(`reader.go:131`). Worst case is therefore a few hundred bytes of heap per ~46
header bytes, i.e. heap ≈ a small multiple of the archive size. Since the whole
directory is read inside `NewReader`, an entry-count or header-size budget
**cannot** be enforced incrementally during parsing; the archive byte-size cap
is the only upstream bound. `[INFERENCE]` for the multiplier arithmetic;
`[VERIFIED]` for the allocation sites and the guard.

### 5.2 Enforcement recipe that works while streaming

1. **Before `NewReader`**: stat/reject archives over the byte cap (also bounds
   §5.1 header memory).
2. **Right after `NewReader`**: `len(r.File)` against the entry-count cap;
   scan every `f.Name` for policy violations (depth, byte length, `..`,
   separators, reserved names, trailing dot/space, case folds, duplicates,
   non-UTF8, NUL) — all of this is free because the directory is already in
   memory; `Σ f.UncompressedSize64` is a cheap upper bound on what extraction
   can produce (the per-entry cap means declared sizes can only *overstate*,
   never understate — and an attacker can inflate them freely), so it is a
   sanity check, not a budget.
3. **Per entry**: `rc, err := f.Open()`; copy through
   `io.LimitReader(rc, remaining+1)` (the `+1` distinguishes “hit the global
   cap” from “used exactly the remainder”). Any read error
   (`ErrFormat`, `ErrChecksum`, `io.ErrUnexpectedEOF`, flate errors) fails the
   entry — never treat a truncated copy as success, because the CRC is only
   checked at EOF (§5.1).
4. **Charge both meters**: decompressed bytes against the global expanded-byte
   cap, and compressed bytes (`f.CompressedSize64`, or the archive size) against
   the ratio budget — `written / compressed`. A ratio derived from *declared*
   sizes is attacker-controlled and therefore worthless as a defense.
5. **Depth/length** must be measured on the raw name bytes, split on both `/`
   and `\` (the name may use either). Byte length is the portable metric;
   Windows limits are in UTF-16 units, Linux `NAME_MAX` is 255 *bytes*.
6. Optional defense in depth: write through `os.Root` (§6) so that even a
   policy bug cannot escape the destination directory.
7. The fs.FS view and `File.OpenRaw` both bypass this policy — extract from
   `Reader.File[i]` + `File.Open` only.

## 6. Long paths, `os` creation semantics and host naming rules

**Go's `os` performs no name validation.** `openFileNolog` checks only
`name == ""` and then calls `fixLongPath(name)` + `syscall.Open`
(`os/file_windows.go:153`); on Unix it is the same shape, and
`syscall.BytePtrFromString` rejects embedded NUL with `EINVAL`
(`syscall/syscall.go:48`). `os.MkdirAll` (`os/path.go:19`) creates parents
one component at a time, each component passed to the OS verbatim. So every
host-specific hazard below is *our* problem, not Go's.

### 6.1 Windows

- **Long paths.** `fixLongPath` (`os/path_windows.go:100`) returns the path
  unchanged when `windows.CanUseLongPaths` is set; otherwise
  `addExtendedPrefix` converts the path (making it absolute first) to the
  `\\?\` (or `\\?\UNC\`) extended form once its length reaches **248 bytes**
  — the 260-12 threshold taken from the MS docs — and passes it to
  `GetFullPathName` + `CreateFileW`. `CanUseLongPaths` is set at runtime by
  `initLongPathSupport` (`runtime/os_windows.go:445`), which on Windows ≥
  10.0.15063 sets the PEB `IsLongPathAwareProcess` bit; the Go source notes the
  flag is undocumented. Go embeds no `longPathAware` manifest (no occurrence of
  `longPathAware` anywhere in the Go source tree).
- **What `\\?\` changes** (MS Learn, *Naming Files, Paths, and Namespaces* /
  *Maximum Path Length Limitation*): it “disable[s] all string parsing and
  send[s] the string that follows it straight to the file system”, which means
  `/` is *not* converted to `\`, `.`/`..` are *not* collapsed, and trailing
  dots/spaces are *not* trimmed. `\\?\` requires a fully qualified path
  (“relative paths are always limited to a total of MAX_PATH characters”) and a
  maximum total length of ~32,767 characters. Practical consequence: **whether
  a hostile name is normalized or preserved literally depends on its length**,
  because only long paths get the `\\?\` treatment. `[INFERENCE]` for that
  length-dependent divergence; the prefixes themselves are documented.
- **Normalization of short paths** (MS Learn, *File path formats on Windows
  systems*, “Path normalization”): `GetFullPathName`-style normalization
  converts `/`→`\`, evaluates `.`/`..`, removes a segment-final single period,
  and removes all trailing periods and spaces when the path does not end in a
  separator; a segment of three or more periods is *not* normalized and is a
  valid name. KB 2829981 adds that the Object Manager itself saves names
  without leading/trailing ASCII space and without a trailing period, while
  retaining other whitespace. `CON`/`COM1`/`LPT1`-style names are converted to
  device paths (`\\.\CON`); Windows 11 stopped prefixing names like `CON.TXT`.
- **Reserved names** are documented as `CON, PRN, AUX, NUL, COM1-COM9, COM¹-³,
  LPT1-LPT9, LPT¹-³` and “avoid these names followed immediately by an
  extension; for example, NUL.txt and NUL.tar.gz are both equivalent to NUL”.
  They apply to directories too. `CONIN$`/`CONOUT$` open console handles.
- **Illegal characters** for a Win32 name: `< > : " / \ | ? *`, NUL, and
  U+0001–U+001F.
- **Case**: “Do not assume case sensitivity … NTFS supports POSIX semantics for
  case sensitivity but this is not the default behavior”; `FILE_FLAG_POSIX_SEMANTICS`
  is the per-file opt-out. Two entries differing only in case therefore collide
  in the same directory, and the second `os.Create` overwrites the first.
- **Unicode**: “no need to perform any Unicode normalization … the file system
  treats path and file names as an opaque sequence of WCHARs”. Non-UTF-8 entry
  names are lossily converted by Go (`encodeWTF16` → U+FFFD), so they are not
  representable.

### 6.2 macOS

- **Case**: APFS “is available in case-sensitive and case-insensitive variants
  on macOS, with case-insensitive being the default”; Apple's current guidance
  is still “Assume directory and filenames are case sensitive. APFS is case
  insensitive by default”. Case-only collisions behave like Windows.
- **Normalization**: “APFS … is normalization-insensitive … normalization
  variants of a filename cannot be created in the same directory”; HFS+ stores
  names “fully decomposed and in canonical order” (Unicode 3.2 rules, up to 255
  `UniChar`). Canonically equivalent names collide on macOS but not on Linux.
- **What is impossible**: POSIX rules apply — no NUL and no `/` in a component;
  `.` and `..` are special (`pathname(7)`; macOS is UNIX 03 certified).
  Trailing dots/spaces are *not* documented as rejected or trimmed (no Apple
  source; `[INFERENCE]` from POSIX “any byte except NUL and /”). `:` is the
  legacy HFS path separator that the system translates; whether it is illegal
  inside an APFS name is `UNVERIFIED` from Apple docs.
- APFS's maximum filename length is `UNVERIFIED` in Apple's documentation;
  HFS+ is 255 UniChar (TN1150).

### 6.3 Linux

- Only `/` and NUL are impossible in a filename (`pathname(7)`); newline and
  control characters are legal (“POSIX.1-2024 encourages implementations to
  disallow … new-line characters. Linux doesn't follow this”).
- `NAME_MAX` = 255 bytes, `PATH_MAX` = 4096 (`include/uapi/linux/limits.h`),
  both advisory via `pathconf(3)`; over-long names fail with `ENAMETOOLONG`.
- Case-sensitive by default; ext4 case-folding is a per-directory opt-in
  (+F attribute, needs the `casefold` feature) that normalizes to NFD
  internally for lookups while preserving the on-disk bytes.
- A name containing `\` is a single ordinary filename; a name containing NUL
  fails with `EINVAL` from Go before the syscall.

### 6.4 Net effect per hostile name class

| Name class | Windows | macOS (default) | Linux |
|---|---|---|---|
| `..` / absolute / drive / UNC | escapes the destination root if joined naively | same | same |
| `C:x` | drive-relative ⇒ resolves against C:'s current directory | literal filename `C:x` | literal filename `C:x` |
| `\` inside a name | separator (extra level) | literal byte in the name | literal byte |
| `CON`, `NUL`, `COM1` | device / `CreateFile` fails or hits a device | literal filename | literal filename |
| `foo.`, `foo ` | trimmed to `foo` ⇒ collision with `foo` | literal filename | literal filename |
| `a.txt` + `A.TXT` | same file | same file | distinct files |
| invalid UTF-8 name | mangled to U+FFFD (`encodeWTF16`), collisions possible | creation fails on APFS (“APFS accepts only valid UTF-8 encoded filenames for creation”) | stored byte-for-byte |
| path > 260 chars | `\\?\` applied by Go ⇒ semantics change (no `.`/`..` expansion, no trimming); >32,767 fails | fine | `PATH_MAX` 4096 |
| component > 255 | ~255 WCHAR component limit | 255 UniChar (HFS+) | `NAME_MAX` 255 bytes ⇒ `ENAMETOOLONG` |

## 7. `filepath.Clean`, `IsLocal`, `Localize`, `fs.ValidPath` on hostile names

- **`filepath.Clean` is purely lexical** and preserves “rootlessness”: a
  relative path never becomes absolute (`internal/filepathlite/path.go:63`,
  reproduced as `filepath.Clean`). `Clean("") == "."`; leading `..` survive
  (`Clean("a/../../b") == "../b"`); on Windows separators are unified to `\`
  and the volume name is left alone except for `/`→`\`
  (`` Clean("//host/share/../x") == `\\host\share\x` `` — doc comment on
  `path/filepath/path.go:37-53`). Windows-only `postClean`
  (`internal/filepathlite/path_windows.go:308`) inserts a `.\` prefix when a
  `:` appears before the first separator (so `a/../c:` stays relative) and a
  `\.` before a leading `\??\`.
- **`filepath.Clean` never confines.** `filepath.Join(root, name)` *cleans* the
  concatenation, so `Join("/dest", "../../etc/passwd") == "/etc/passwd"`. The
  documented guarantee is conditional: “If `IsLocal(path)` returns true, then
  `Join(base, path)` will always produce a path contained within `base`”
  (`path/filepath/path.go:63-72`).
- **`filepath.IsLocal`** (`path/filepath/path.go:73`) — the check
  `archive/zip` uses — is lexical and OS-dependent:
  - Windows (`internal/filepathlite/path_windows.go:24` `isLocal`): rejects
    empty; rejects a leading path separator; rejects **any `:` anywhere**
    (“Rejecting any path with a colon is conservative but safe”); rejects any
    element for which `isReservedName` is true; then `Clean`s when `.`/`..`
    elements were seen and rejects if the result is `..` or starts with `..\`.
  - Unix (`internal/filepathlite/path.go:146` `unixIsLocal`): rejects empty and
    absolute; rejects if the cleaned path is `..` or starts with `../`.
    Backslashes are *not* rejected on Unix — hence the explicit
    `strings.Contains(name, "\\")` in the zip check.
  - `isReservedName` (`path_windows.go:96`) strips at the first `:` or `.`,
    trims trailing spaces, then matches `CON`/`PRN`/`AUX`/`NUL`, `COM1`-`COM9`,
    `LPT1`-`LPT9` (plus superscript ¹²³ variants) and `CONIN$`/`CONOUT$`; when
    the name had an extension it defers to the OS
    (`RtlIsDosDeviceName_U`), which is how “Windows 11 no longer reserves
    `CON.txt`” is handled. Note it rejects `NUL ` (trailing space) but says
    nothing about trailing dots (`foo.` passes).
- **`filepath.Localize`** (`path/filepath/path.go:85`,
  `internal/filepathlite/path.go:168`) is the sanctioned fs.FS-path → OS-path
  conversion: requires `fs.ValidPath`, then on Windows rejects `:`, `\` and NUL
  and every reserved-name element (`localize`); on Unix it only rejects NUL.
  The doc comment warns that it returns an error for paths the OS cannot
  represent, e.g. any name containing `\` on Windows.
- **`fs.ValidPath`** (`io/fs/fs.go:41`) rejects `""`, `.`/`..` elements, empty
  elements, and leading/trailing slashes; it **accepts** `:` and `\`
  (“Paths containing other characters such as backslash and colon are accepted
  as valid, but those characters must never be interpreted by an FS
  implementation as path element separators”). That is why the zip fs view can
  hand back `C:/x` and `?/C:/x`.
- `filepath.HasPrefix` (`path/filepath/path_windows.go:17`) is the
  case-insensitive, separator-insensitive prefix helper — relevant when
  checking containment on Windows; it is not a confinement primitive by itself.
- NUL bytes: `os` rejects them before reaching the OS —
  `syscall.ByteSliceFromString` returns `EINVAL` on embedded NUL
  (`syscall/syscall.go:48`) and `syscall.UTF16FromString` likewise
  (`syscall/syscall_windows.go:43`).

## 8. Limits comparable tools document

Two different things are often conflated: *path* policy (what names are
allowed) and *resource* policy (how much output is allowed). Every mainstream
extractor documents the first; almost none documents the second.

### 8.1 Path policy

| Tool | Documented behaviour on extract |
|---|---|
| 7-Zip CLI | Absolute/UNC/drive paths are reduced to relative paths unless `-spf` (“If -spf switch is not specified … 7-Zip converts paths to relative paths when you extract archive”), and the `-spf` page warns it “can try to rewrite any file with path specified in archive”. `-snt[-]` “Replace trail dots and spaces in file names for Extract operation” — a dedicated trailing-dot/space normalizer for extraction. `-snl` “Store symbolic links as links (WIM and TAR formats only)”, i.e. the *store* switch is not a ZIP feature; extract-time link restoration is on by default in `ArchiveCommandLine.cpp` unless disabled, guarded since 25.01 by the dangerous-link check (`-snld`, CVE-2025-55188) whose predicate refuses absolute targets (`!levelsInfo.IsAbsolute && levelsInfo.LowLevel >= 0`). |
| Info-ZIP `unzip` | `-:` documents that “For security reasons, unzip normally removes ‘parent dir’ path components ( `../` ) from the names of extracted file. This safety feature (new for version 5.50)…”. The upstream FAQ records that all versions through 5.42 were traversal-vulnerable (leading `/` or `..` entries), fixed in 5.50/5.51. |
| libarchive `bsdtar` | Man page SECURITY: leading `/` is removed by default, entries whose pathnames “contain `..`” are refused, and entries whose target directory “would be altered by a symlink” are refused; `-P` suppresses all three. The *library* default is the opposite: `ARCHIVE_EXTRACT_SECURE_NOABSOLUTEPATHS`, `…SECURE_NODOTDOT` and `…SECURE_SYMLINKS` all document “The default is to not refuse such paths / not to perform this check”. |
| .NET `ZipFile.ExtractToDirectory` | Sanitizes the entry name, then `Path.GetFullPath(Path.Combine(destination, name))` and requires the result to start with `destination + Path.DirectorySeparatorChar` (trailing separator deliberate), otherwise `IOException` — “Extracting Zip entry would have resulted in a file outside the specified destination directory.” An entry whose final name component is empty is treated as a directory, and a non-empty entry with such a name throws the “directory name with data” error. Per-entry APIs (`ZipArchiveEntry.ExtractToFile`) do **not** perform this validation (documented in the .NET zip/tar best-practices page). |
| Python `zipfile` | `extract`/`extractall` strip absolute/drive/UNC prefixes and leading (back)slashes, remove every `..` component, and on Windows replace `: < > | " ? *` with `_`; `extractall`’s doc warns that files can still be created outside `path` and points at `extract`. `zipfile.Path` “does not sanitize filenames … it is the caller’s responsibility”. |
| Rust `zip` crate | The API itself carries the policy: `enclosed_name()` guarantees no NUL, no escape and no absolute path; `mangled_name()` rewrites instead; `sanitized_name()` was deprecated because “by stripping `..`s from the path, the meaning of paths can change”. `ZipArchive::extract()` sanitizes with `enclosed_name` and creates/follows symlinks only when the target is inside the destination (checked with `canonicalize`). It also exposes `encrypted()`, `is_symlink()`, `is_dir()`, `unix_mode()` — a per-entry reporting model worth copying. |
| Windows Explorer | No first-party documented path policy beyond the Win32 rules in §6; nothing found on per-archive limits at all (Microsoft’s end-user zip page documents behaviour, not caps). |

### 8.2 Resource budgets

| Tool | Size | Count | Ratio | Depth / time |
|---|---|---|---|---|
| ClamAV `clamd.conf` | `MaxFileSize` **100M** (input and every inner file), `MaxScanSize` **400M** cumulative — “The size of an archive plus the sum of the sizes of all files within archive count toward the scan size”; all sizes capped at 4 GB | `MaxFiles` **10000** | no ratio directive exists | `MaxRecursion` **17** (nested archives), `MaxDirectoryRecursion` 15, `MaxScanTime` 120000 ms; exceeding a limit is reported as `Heuristics.Limits.Exceeded` when `AlertExceedsMax` is set |
| Apache POI `ZipSecureFile` (OOXML bomb guard) | `DEFAULT_MAX_ENTRY_SIZE` = 4 GiB (32-bit zip max) | `DEFAULT_MAX_FILE_COUNT` = 1000 | **`MIN_INFLATE_RATIO` = 0.01** — “Sets the ratio between de- and inflated bytes to detect zipbomb. It defaults to 1%” — the only first-party *ratio* default found | `DEFAULT_GRACE_ENTRY_SIZE` = 100 KiB slack before the ratio check trips |
| Go `golang.org/x/mod/zip` (module zips) | `MaxZipFile` = 500 MiB total compressed **and** total uncompressed; `MaxGoMod`/`MaxLICENSE` 16 MiB | — | — | — |
| Info-ZIP format table | 4 GB per file, 256 TB per archive (format limits) | 65,536 entries (format; “not a hard limit”) | — | path/name 64 KB (format); 2 GB practical on hosts whose `fseek()` is 32-bit |
| 7-Zip, .NET, Python, Rust `zip`, libarchive | no documented size/count/ratio default found in first-party docs or source; Python states the policy explicitly (“we should improve documentation… not limit extraction by default”, bpo-36260, docs-only resolution for CVE-2019-9674) | | | |

### 8.3 Mod managers

The two closest functional comparables both delegate archive handling and
document no budget at all:

- **Mod Organizer 2** extracts through the separate
  `ModOrganizer2/modorganizer-archive` library, which wraps the 7-Zip `7z.dll`
  (archive enumeration via `getFileList()`, extraction via `Archive::extract()`
  with progress/error callbacks and `Archive::cancel()`). Neither the archive
  library's README nor MO2's documentation states a maximum uncompressed size,
  entry count, compression ratio, nesting depth or extraction time, and the
  archive layer has no symlink policy of its own — archive symlink behaviour is
  whatever the bundled 7-Zip version does (and 7-Zip changed that behaviour in
  25.01, see above). So an MO2 release's safety depends on its bundled 7-Zip.
- **Vortex** documents the same 7-Zip-backed extraction into a staging folder,
  followed by a separate deployment step (hardlink/symlink/copy) with a
  deployment manifest. Its public documentation contains no ZIP-bomb quota
  either; the symlink documentation is about *deployment* links Vortex creates,
  not about symlinks inside a downloaded archive.

Both absences are "not found in first-party docs" (searched 2026-09-26), not
statements that the code has no limits. The practical takeaway is the same as
§8.2: a mod manager that wants a bound has to impose it itself.

### 8.4 What that means for our defaults

- The only widely-used, first-party **ratio** default is POI’s 1% inflate ratio
  with a 100 KiB grace per entry; for an archive manager a per-entry *and*
  archive-wide ratio computed from bytes actually written vs bytes read from the
  archive (§5.2) is the enforceable analogue.
- ClamAV’s model is the closest structural fit for the PRD §13 list: per-file
  size, cumulative scan size, file count, recursion depth and wall-clock time,
  each with an explicit default and a distinct “limit exceeded” outcome. Its
  cumulative-size definition (“archive plus sum of inner files”) is worth
  copying for the expanded-bytes budget because it charges the container itself,
  not just the payload.
- Nothing in the field relies on library-side limits; every tool that is safe
  does the policy in the extractor (or offers it as an option that is on by
  default at the CLI but off in the library, as libarchive shows).

## 9. Consequences for the reject-list ticket

Everything below follows from §1-§8; it is the input the parent ticket asked
for, not a decision.

1. **Policy must be our own scan over `Reader.File`.** `ErrInsecurePath` cannot
   name the offending entry, is opt-in, and is OS-dependent; the fs.FS view
   silently rewrites names and drops some. `r.File[i]` is the only faithful
   view, and the scan is O(entries) with no I/O.
2. **Never combine `r.File[i].Name` with `filepath.Join`.** Use
   `filepath.Localize` for names that pass policy, and treat `IsLocal` as a
   *necessary but insufficient* filter: it misses trailing dots/spaces,
   duplicate names, case-only collisions, encrypted entries and symlink modes,
   and on Unix it misses backslashes entirely.
3. **Per-entry reporting is available for everything except `ErrInsecurePath`**:
   name (raw bytes), `Mode()` (symlink/dir/exec/setuid), `Flags` (bit 0 =
   encrypted, bit 3 = descriptor), `Method`, `NonUTF8`, `CreatorVersion`,
   `CompressedSize64`/`UncompressedSize64`, `ExternalAttrs`, `Comment`. Whatever
   the reject list ends up being, the typed error can always carry entry name +
   reason.
4. **Limits need four enforcement points**: archive bytes (pre-parse, bounds
   header memory), entry count + name scans + declared-size sum (post-parse,
   pre-extraction), streamed expansion bytes + real compression ratio
   (per-entry copy loop), and depth/length (name scan). None of them can be
   delegated to `archive/zip`.
5. **Integrity**: always read to EOF so `ErrChecksum`/`ErrUnexpectedEOF` can
   fire; a `io.LimitReader`-truncated copy is indistinguishable from corrupt
   data unless you compare the copied byte count against `UncompressedSize64`.
6. **Host-specific hazards that Go will not stop for us** (§4/§6): trailing
   dots and spaces and reserved device names on Windows, case-insensitive
   collisions on Windows and macOS, silent U+FFFD mangling of non-UTF-8 names
   on Windows, path-length overflow on Windows unless the `\\?\` form is used,
   and NUL (rejected by the OS, but only at creation time — the entry looks
   fine until then).

## Sources

Go 1.25.14 toolchain source (`golang.org/toolchain@v0.0.1-go1.25.14`):

- `src/archive/zip/reader.go` — `Reader.init`, `OpenReader`, `NewReader`,
  `File.Open`, `File.OpenRaw`, `checksumReader.Read`, `findBodyOffset`,
  `readDirectoryHeader`, `readDataDescriptor`, `readDirectoryEnd`,
  `findDirectory64End`, `readDirectory64End`, `toValidName`, `initFileList`,
  `Reader.Open`, `openLookup`, `openDir.ReadDir`.
- `src/archive/zip/struct.go` — `FileHeader` doc (“must be a relative path…”),
  `Mode`, `SetMode`, `msdosModeToFileMode`, `unixModeToFileMode`,
  `hasDataDescriptor`, `isZip64`.
- `src/archive/zip/writer.go` — `Create` doc comment, `CreateHeader`,
  `writeHeader` (`errLongName`), `detectUTF8`.
- `src/archive/zip/register.go` — Store/Deflate registry, flate pooling.
- `src/archive/zip/reader_test.go`, `zip_test.go` — `TestCVE202127919`,
  `TestCVE202133196`, `TestCVE202139293`, `TestCVE202141772`, `TestUnderSize`,
  `TestIssue54801`, `TestInsecurePaths`, `TestDisableInsecurePathCheck`,
  `TestCompressedDirectory`, `TestFS`, `TestFSWalk`, `TestFSWalkBadFile`,
  `TestHeaderTooLongErr`.
- `src/internal/godebugs/table.go` — `zipinsecurepath` default.
- `src/internal/filepathlite/path.go`, `path_windows.go`, `path_unix.go` —
  `Clean`, `unixIsLocal`, `isLocal` (Windows), `isReservedName`,
  `isReservedBaseName`, `volumeNameLen`, `postClean`, `localize`.
- `src/path/filepath/path.go` — `Clean`/`IsLocal`/`Localize`/`Join` docs,
  `HasPrefix` (Windows).
- `src/io/fs/fs.go` — `ValidPath` docs.
- `src/os/path_windows.go` — `fixLongPath`, `addExtendedPrefix`.
- `src/os/file_windows.go`, `src/os/path.go` — `openFileNolog`, `MkdirAll`.
- `src/os/root.go`, `root_windows.go` — `Root` docs, `splitPathInRoot`,
  `rootCleanPath`, `isValidRootFSPath`.
- `src/runtime/os_windows.go` — `initLongPathSupport` (PEB
  `IsLongPathAwareProcess`).
- `src/syscall/syscall.go`, `syscall_windows.go`, `wtf8_windows.go` —
  `ByteSliceFromString`, `UTF16FromString`, `encodeWTF16`; `CreateFileW`
  binding.

Go documentation / release notes:

- `go.dev/doc/go1.20` — `zipinsecurepath`/`ErrInsecurePath` wording (“A future
  version of Go may disable insecure paths by default”), `IsLocal`
  introduction.
- `go.dev/doc/go1.24` — `os.Root` introduction (“do not permit paths that refer
  to locations outside the directory, including ones that follow symbolic links
  out of the directory”).

Host platform documentation:

- Microsoft Learn, *Naming Files, Paths, and Namespaces*
  (`learn.microsoft.com/windows/win32/fileio/naming-a-file`) — reserved names
  list, illegal characters, `\\?\` semantics, case-insensitivity, trailing
  dot/space rule.
- Microsoft Learn, *Maximum Path Length Limitation*
  (`…/windows/win32/fileio/maximum-file-path-limitation`) — MAX_PATH 260,
  `\\?\` 32,767, `/`→`\` conversion, relative paths always MAX_PATH.
- Microsoft Learn, *File path formats on Windows systems*
  (`learn.microsoft.com/dotnet/standard/io/file-path-formats`) — path
  normalization steps (segment-final period removal, trailing period/space
  trimming), device-path exception, drive-relative `C:x` semantics, Windows 11
  legacy-device change.
- Microsoft KB 2829981 (troubleshoot/windows-client/shell-experience/
  file-folder-name-whitespace-characters) — Object Manager trimming of leading
  and trailing ASCII space and trailing period.
- Apple, *Apple File System Guide* FAQ — APFS case variants, normalization
  insensitivity, UTF-8 requirement.
- Apple TN1150 — HFS+ 255 `HFSUniStr255`, decomposed/canonical storage, HFSX.
- Apple, *File System Comparisons* / `CFURLPathStyle` — `:` as the legacy HFS
  path separator.
- POSIX XBD 3.146/4.16 (`pubs.opengroup.org/onlinepubs/9799919799/basedefs/`) —
  filename bytes may not contain NUL or `/`; dot/dot-dot special.
- `man7.org` `pathname(7)`, `path_resolution(7)`, `pathconf(3)` — Linux
  name rules, `ENAMETOOLONG`, advisory limits.
- Linux `include/uapi/linux/limits.h` — `NAME_MAX` 255, `PATH_MAX` 4096.
- Linux `Documentation/admin-guide/ext4.rst` — per-directory casefold opt-in.

Comparable tools:

- 7-Zip CHM manual mirrors (`7-zip.opensource.jp/chm/cmdline/switches/`:
  `index.htm`, `spf.htm`) and upstream source
  (`github.com/ip7z/7zip` `CPP/7zip/UI/Common/ArchiveCommandLine.cpp`,
  `ArchiveExtractCallback.cpp`, `DOC/src-history.txt`) — `-spf`, `-snt`, `-snl`,
  `-snld`, CVE-2025-55188 note. The upstream repo hosts no HTML manual, so the
  quoted switch text comes from verbatim mirrors of the shipped CHM.
- Info-ZIP `unzip(1)` and FAQ (`infozip.sourceforge.net/FAQ.html`) — `-:`,
  traversal history, format limit table.
- libarchive `tar/bsdtar.1` and `libarchive/archive_write_disk.3`
  (`github.com/libarchive/libarchive`) — SECURITY section, `-P`, and the
  library-level “default is not to refuse” flags.
- dotnet/runtime
  (`src/libraries/System.IO.Compression.ZipFile/src/System/IO/Compression/ZipFileExtensions.ZipArchiveEntry.Extract.cs`,
  `…/ZipFileExtensions.ZipArchive.Extract.cs`, `Resources/Strings.resx`) —
  containment check and `IO_ExtractingResultsInOutside`; `ArchivingUtils`
  sanitizer as rendered on source.dot.net; dotnet/docs
  `zip-tar-best-practices.md` — per-entry APIs skip the validation.
- Python `zipfile` docs (`docs.python.org/3/library/zipfile.html`) — `extract`
  sanitization note, `extractall` warning, `zipfile.Path` responsibility,
  “Decompression pitfalls”; bpo-36260 / CVE-2019-9674 for the deliberate lack
  of limits.
- Rust `zip` crate docs (`docs.rs/zip/latest`, source view of `src/read.rs`) —
  `enclosed_name`, `mangled_name`, deprecated `sanitized_name`,
  `ZipArchive::extract`, `encrypted`/`is_symlink`/`unix_mode`.
- ClamAV `docs/man/clamd.conf.5.in` and `docs/man/clamscan.1.in`
  (`github.com/Cisco-Talos/clamav`) — `MaxFileSize`, `MaxScanSize`,
  `MaxFiles`, `MaxRecursion`, `MaxScanTime`, `AlertExceedsMax`.
- Apache POI `poi-ooxml/…/ZipSecureFile.java` and its Javadoc — inflate-ratio
  and entry-size/count defaults.
- `golang.org/x/mod/zip/zip.go` — `MaxZipFile` 500 MiB caps.

Explicitly unverified in this doc: whether macOS rejects trailing dots/spaces or
control characters (no Apple source found; POSIX implies acceptance), whether
`:` is illegal inside an APFS/HFS+ name, APFS’s maximum filename length, any
Microsoft statement that reserved-name rules changed in Windows 11 (only the
.NET `Path.GetFullPath` page says so), any documented size/count/ratio limit for
Windows Explorer, and the historical ClamAV `ArchiveMaxCompressionRatio`
removed before current docs.
