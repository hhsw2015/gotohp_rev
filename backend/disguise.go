package backend

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Google Photos "original quality" storage does not re-encode uploads, so a
// media file with arbitrary bytes appended after its normal end-of-container
// will survive a full download round-trip. This module hides any file as an
// MP4 by concatenating a tiny valid MP4 cover, a magic separator, a filename
// header, and the payload.
//
// Wire format after HideAsMP4:
//   [minimal valid MP4]  [disguiseSeparator]  [uint32 LE filename length]  [filename]  [payload]
//
// The separator matches xob0t/gp_disguise's Python default so payloads are
// interchangeable between the two tools.

const (
	// disguiseSeparator must match gp_disguise (Python) for interop.
	disguiseSeparator = "FILE_DATA_BEGIN"

	// disguiseSuffix is the extension appended when disguising an input.
	disguiseSuffix = ".mp4"

	// disguiseMaxNameLen bounds the embedded filename length to defend
	// against a garbage trailer whose "length" field is random.
	disguiseMaxNameLen = 4096

	// disguiseTailSearchWindow is how much we scan from the end of the
	// file when looking for the trailer. The trailer sits right after the
	// MP4 cover, so a payload of any size still keeps the magic near the
	// front, but we defensively look both ends via seek so we can bail
	// early on plain media files without loading the whole file into RAM.
	disguiseTailSearchWindow = 64 * 1024
)

// IsMediaFilename returns true if the extension is one Google Photos accepts
// natively (i.e. we do NOT need to disguise the file).
func IsMediaFilename(path string) bool {
	return isSupportedByGooglePhotos(path)
}

// mp4TemplateBase64 is a 1570-byte 64x64 1-frame H.264 MP4 generated once via
// ffmpeg. Embedding it here keeps gotohp dependency-free at runtime.
const mp4TemplateBase64 = "" +
	"AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDEAAAAIZnJlZQAAAuVtZGF0AAACrQYF//+p" +
	"3EXpvebZSLeWLNgg2SPu73gyNjQgLSBjb3JlIDE2NSByMzIyMiBiMzU2MDVhIC0gSC4yNjQvTVBF" +
	"Ry00IEFWQyBjb2RlYyAtIENvcHlsZWZ0IDIwMDMtMjAyNSAtIGh0dHA6Ly93d3cudmlkZW9sYW4u" +
	"b3JnL3gyNjQuaHRtbCAtIG9wdGlvbnM6IGNhYmFjPTEgcmVmPTMgZGVibG9jaz0xOjA6MCBhbmFs" +
	"eXNlPTB4MzoweDExMyBtZT1oZXggc3VibWU9NyBwc3k9MSBwc3lfcmQ9MS4wMDowLjAwIG1peGVk" +
	"X3JlZj0xIG1lX3JhbmdlPTE2IGNocm9tYV9tZT0xIHRyZWxsaXM9MSA4eDhkY3Q9MSBjcW09MCBk" +
	"ZWFkem9uZT0yMSwxMSBmYXN0X3Bza2lwPTEgY2hyb21hX3FwX29mZnNldD0tMiB0aHJlYWRzPTIg" +
	"bG9va2FoZWFkX3RocmVhZHM9MSBzbGljZWRfdGhyZWFkcz0wIG5yPTAgZGVjaW1hdGU9MSBpbnRl" +
	"cmxhY2VkPTAgYmx1cmF5X2NvbXBhdD0wIGNvbnN0cmFpbmVkX2ludHJhPTAgYmZyYW1lcz0zIGJf" +
	"cHlyYW1pZD0yIGJfYWRhcHQ9MSBiX2JpYXM9MCBkaXJlY3Q9MSB3ZWlnaHRiPTEgb3Blbl9nb3A9" +
	"MCB3ZWlnaHRwPTIga2V5aW50PTI1MCBrZXlpbnRfbWluPTEgc2NlbmVjdXQ9NDAgaW50cmFfcmVm" +
	"cmVzaD0wIHJjX2xvb2thaGVhZD00MCByYz1jcmYgbWJ0cmVlPTEgY3JmPTIzLjAgcWNvbXA9MC42" +
	"MCBxcG1pbj0wIHFwbWF4PTY5IHFwc3RlcD00IGlwX3JhdGlvPTEuNDAgYXE9MToxLjAwAIAAAAAo" +
	"ZYiEABX//uzPfgU28zmL6jO9JxydeOY06aFdOh0hhIVsUAt6pJMwgQAAAxVtb292AAAAbG12aGQA" +
	"AAAAAAAAAAAAAAAAAAPoAAAD6AABAAABAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAA" +
	"AAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACAAACQHRyYWsAAABcdGto" +
	"ZAAAAAMAAAAAAAAAAAAAAAEAAAAAAAAD6AAAAAAAAAAAAAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAA" +
	"AAEAAAAAAAAAAAAAAAAAAEAAAAAAQAAAAEAAAAAAACRlZHRzAAAAHGVsc3QAAAAAAAAAAQAAA+gA" +
	"AAAAAAEAAAAAAbhtZGlhAAAAIG1kaGQAAAAAAAAAAAAAAAAAAEAAAABAAFXEAAAAAAAtaGRscgAA" +
	"AAAAAAAAdmlkZQAAAAAAAAAAAAAAAFZpZGVvSGFuZGxlcgAAAAFjbWluZgAAABR2bWhkAAAAAQAA" +
	"AAAAAAAAAAAAJGRpbmYAAAAcZHJlZgAAAAAAAAABAAAADHVybCAAAAABAAABI3N0YmwAAAC/c3Rz" +
	"ZAAAAAAAAAABAAAAr2F2YzEAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAAAQABAAEgAAABIAAAAAAAA" +
	"AAEVTGF2YzYyLjExLjEwMCBsaWJ4MjY0AAAAAAAAAAAAAAAY//8AAAA1YXZjQwFkAAr/4QAYZ2QA" +
	"CqzZRCbARAAAAwAEAAADAAg8SJZYAQAGaOvjyyLA/fj4AAAAABBwYXNwAAAAAQAAAAEAAAAUYnRy" +
	"dAAAAAAAABboAAAAAAAAABhzdHRzAAAAAAAAAAEAAAABAABAAAAAABxzdHNjAAAAAAAAAAEAAAAB" +
	"AAAAAQAAAAEAAAAUc3RzegAAAAAAAALdAAAAAQAAABRzdGNvAAAAAAAAAAEAAAAwAAAAYXVkdGEA" +
	"AABZbWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAAAAAAAAAsaWxzdAAAACSp" +
	"dG9vAAAAHGRhdGEAAAABAAAAAExhdmY2Mi4zLjEwMA=="

var mp4Template []byte

func init() {
	b, err := base64.StdEncoding.DecodeString(mp4TemplateBase64)
	if err != nil {
		panic(fmt.Sprintf("disguise: failed to decode embedded MP4 template: %v", err))
	}
	mp4Template = b
}

// DisguiseSize returns the total byte size of the disguised container for a
// payload of `srcSize` bytes. Used by upload to preflight `Content-Length`
// without materializing the file on disk.
func DisguiseSize(originalName string, srcSize int64) int64 {
	return int64(len(mp4Template)) + int64(len(disguiseSeparator)) + 4 + int64(len(originalName)) + srcSize
}

// OpenDisguiseReader returns an io.ReadCloser that streams the disguised
// representation of `src` without touching the disk. The reader owns the open
// file handle to `src` and closes it when the caller calls Close(). `srcName`
// is the filename to embed in the trailer (typically filepath.Base(src)).
func OpenDisguiseReader(src, srcName string) (io.ReadCloser, int64, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, 0, fmt.Errorf("disguise: refusing to hide a directory (%s)", src)
	}

	var trailer bytes.Buffer
	trailer.WriteString(disguiseSeparator)
	_ = binary.Write(&trailer, binary.LittleEndian, uint32(len(srcName)))
	trailer.WriteString(srcName)

	total := int64(len(mp4Template)) + int64(trailer.Len()) + info.Size()
	r := io.MultiReader(
		bytes.NewReader(mp4Template),
		bytes.NewReader(trailer.Bytes()),
		f,
	)
	return &disguiseReader{r: r, f: f}, total, nil
}

type disguiseReader struct {
	r io.Reader
	f *os.File
}

func (d *disguiseReader) Read(p []byte) (int, error) { return d.r.Read(p) }
func (d *disguiseReader) Close() error               { return d.f.Close() }

// HideAsMP4 writes an MP4-disguised copy of src to dst. If dst is empty a
// sibling `<src>.mp4` path in os.TempDir is used, chosen to avoid clobbering
// pre-existing files. The returned path is the file to upload. Callers should
// os.Remove(returned path) when done.
//
// Prefer OpenDisguiseReader for large payloads to skip the disk round-trip.
func HideAsMP4(src, dst string) (string, error) {
	rc, _, err := OpenDisguiseReader(src, filepath.Base(src))
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()

	var out *os.File
	if dst == "" {
		out, err = os.CreateTemp("", "gotohp-disguise-*"+disguiseSuffix)
		if err != nil {
			return "", err
		}
	} else {
		out, err = os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
	}
	dstPath := out.Name()

	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		_ = os.Remove(dstPath)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dstPath)
		return "", fmt.Errorf("disguise: close output: %w", err)
	}
	return dstPath, nil
}

// TryExtractDisguised inspects the file at path and, if it carries a
// gp_disguise payload, streams the original bytes to outDir/<originalName>.
// Returns (restoredPath, true, nil) on success, ("", false, nil) if the file
// carries no payload, and ("", false, err) on I/O errors or corrupt payloads.
//
// The function seeks to the expected trailer position (right after the MP4
// cover) so it does not load the whole file into memory even for multi-GB
// payloads, and it will not misfire on plain media files whose bodies
// happen to contain the separator elsewhere.
func TryExtractDisguised(path, outDir string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	minSize := int64(len(mp4Template)) + int64(len(disguiseSeparator)) + 4
	if info.Size() < minSize {
		return "", false, nil
	}

	// Read a window right after the template to look for the separator.
	// Cover + trailer header should live in the first
	//   len(mp4Template) + disguiseTailSearchWindow bytes.
	headLen := int64(len(mp4Template)) + disguiseTailSearchWindow
	if headLen > info.Size() {
		headLen = info.Size()
	}
	head := make([]byte, headLen)
	if _, err := io.ReadFull(f, head); err != nil {
		return "", false, err
	}

	sepBytes := []byte(disguiseSeparator)
	// A disguised file always has the trailer at the very start of the
	// bytes we appended, i.e. right after the template. Look for the
	// FIRST occurrence at or after len(mp4Template); refuse to match a
	// coincidental sequence earlier inside the cover.
	searchFrom := 0
	if len(mp4Template) < len(head) {
		searchFrom = len(mp4Template) - len(sepBytes)
		if searchFrom < 0 {
			searchFrom = 0
		}
	}
	sepIdx := bytes.Index(head[searchFrom:], sepBytes)
	if sepIdx < 0 {
		return "", false, nil
	}
	sepIdx += searchFrom

	// Parse name-len + name from head; if head didn't cover the whole
	// filename (very unusual: names > disguiseTailSearchWindow), refuse.
	nameLenOffset := sepIdx + len(sepBytes)
	if nameLenOffset+4 > len(head) {
		return "", false, nil
	}
	nameLen := binary.LittleEndian.Uint32(head[nameLenOffset : nameLenOffset+4])
	if nameLen == 0 || nameLen > disguiseMaxNameLen {
		return "", false, nil
	}
	nameStart := nameLenOffset + 4
	nameEnd := nameStart + int(nameLen)
	if nameEnd > len(head) {
		return "", false, nil
	}
	name := string(head[nameStart:nameEnd])
	safeName, ok := sanitizeEmbeddedName(name)
	if !ok {
		return "", false, fmt.Errorf("disguise: unsafe embedded filename %q", name)
	}

	// Seek payload start and stream to disk.
	payloadStart := int64(nameEnd)
	payloadLen := info.Size() - payloadStart
	if payloadLen < 0 {
		return "", false, fmt.Errorf("disguise: negative payload length")
	}
	if _, err := f.Seek(payloadStart, io.SeekStart); err != nil {
		return "", false, err
	}

	if outDir == "" {
		outDir = filepath.Dir(path)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", false, err
	}
	outPath, out, err := createUniquely(outDir, safeName)
	if err != nil {
		return "", false, err
	}
	if _, err := io.CopyN(out, f, payloadLen); err != nil {
		_ = out.Close()
		_ = os.Remove(outPath)
		return "", false, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(outPath)
		return "", false, err
	}
	return outPath, true, nil
}

// sanitizeEmbeddedName rejects filenames that could path-traverse or otherwise
// escape outDir. Returns the safe basename and true, or "" and false if the
// name is dangerous. This is stricter than filepath.Base alone — `..` slips
// past Base, and Windows drive letters / backslashes need explicit rejection
// even on Unix hosts because the trailer may have been produced elsewhere.
func sanitizeEmbeddedName(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if strings.ContainsAny(name, "/\\") {
		return "", false
	}
	if strings.Contains(name, "..") {
		return "", false
	}
	if strings.ContainsRune(name, ':') {
		return "", false
	}
	if name == "." {
		return "", false
	}
	// filepath.Base as a last-line-of-defense (harmless if already clean).
	base := filepath.Base(name)
	if base == "." || base == ".." || base == "" {
		return "", false
	}
	return base, true
}

// createUniquely opens outDir/name with O_EXCL; on collision it appends
// `-1`, `-2`, ... before the extension until it finds a free path, so a
// disguised file never silently overwrites an unrelated user file.
func createUniquely(outDir, name string) (string, *os.File, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 10000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		p := filepath.Join(outDir, candidate)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			return p, f, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("disguise: could not find a unique name for %s", name)
}
