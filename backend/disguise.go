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

// HideAsMP4 writes an MP4-disguised copy of src to dst. If dst is empty a
// sibling `<src>.mp4` path is used. The returned path is the file to upload.
func HideAsMP4(src, dst string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("disguise: refusing to hide a directory (%s)", src)
	}

	if dst == "" {
		dst = src + disguiseSuffix
	}

	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()

	// 1. MP4 cover (embedded template).
	if _, err := out.Write(mp4Template); err != nil {
		_ = os.Remove(dst)
		return "", err
	}

	// 2. Separator + filename length + filename.
	filename := filepath.Base(src)
	if _, err := out.WriteString(disguiseSeparator); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	if err := binary.Write(out, binary.LittleEndian, uint32(len(filename))); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	if _, err := out.WriteString(filename); err != nil {
		_ = os.Remove(dst)
		return "", err
	}

	// 3. Payload.
	in, err := os.Open(src)
	if err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	defer func() { _ = in.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

// TryExtractDisguised inspects the file at path and, if it carries a
// gp_disguise payload, writes the original bytes to outDir/<originalName>.
// Returns (restoredPath, true, nil) on success, ("", false, nil) if the file
// is a plain media file with no payload, and ("", false, err) on I/O errors
// or corrupt payloads.
func TryExtractDisguised(path, outDir string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	sepIdx := bytes.LastIndex(data, []byte(disguiseSeparator))
	if sepIdx < 0 {
		return "", false, nil
	}
	rest := data[sepIdx+len(disguiseSeparator):]
	if len(rest) < 4 {
		return "", false, errors.New("disguise: truncated after separator")
	}
	nameLen := binary.LittleEndian.Uint32(rest[:4])
	if nameLen == 0 || nameLen > 4096 {
		return "", false, fmt.Errorf("disguise: implausible filename length %d", nameLen)
	}
	if uint32(len(rest)) < 4+nameLen {
		return "", false, errors.New("disguise: truncated in filename")
	}
	name := string(rest[4 : 4+nameLen])
	payload := rest[4+nameLen:]

	if outDir == "" {
		outDir = filepath.Dir(path)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", false, err
	}
	// Guard against basename containing path separators from a malicious source.
	safeName := filepath.Base(name)
	if safeName == "." || safeName == string(filepath.Separator) {
		return "", false, errors.New("disguise: invalid embedded filename")
	}
	outPath := filepath.Join(outDir, safeName)
	if err := os.WriteFile(outPath, payload, 0o644); err != nil {
		return "", false, err
	}
	return outPath, true, nil
}
