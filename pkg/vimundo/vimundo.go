// Package vimundo reads Vim and Neovim persistent undo files.
package vimundo

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

var (
	ErrNotUndoFile = errors.New("not a Vim undo file")
	ErrEncrypted   = errors.New("encrypted Vim undo files are not supported")
	ErrExtmarks    = errors.New("unsupported Neovim undo extmark")
)

const (
	version           = 2
	encryptedVersion  = 0x8002
	headerMagic       = 0x5fd0
	headerEndMagic    = 0xe7aa
	entryMagic        = 0xf518
	entryEndMagic     = 0x3581
	markCount         = 26
	maxStringLength   = 128 << 20
	maxItems          = 1 << 20
	extmarkDataLength = 48 // ExtmarkSplice and ExtmarkMove are both 48 bytes in Neovim's file format.
)

var startMagic = []byte{'V', 'i', 'm', 0x9f, 'U', 'n', 'D', 'o', 0xe5}

type File struct {
	Version         uint16
	Hash            [32]byte
	LineCount       uint32
	SavedLine       string
	SavedLineNumber uint32
	SavedLineColumn uint32
	OldHead         uint32
	NewHead         uint32
	CurrentHead     uint32
	SequenceLast    uint32
	SequenceCurrent uint32
	Time            time.Time
	LastSaveNumber  uint32
	Headers         []Header
}

type Header struct {
	Next, Previous, AltNext, AltPrevious uint32
	Sequence                             uint32
	Cursor                               Position
	CursorVirtualColumn                  int32
	Flags                                uint16
	Marks                                [markCount]Position
	Visual                               Visual
	Time                                 time.Time
	SaveNumber                           uint32
	Entries                              []Entry
}

type Position struct{ Line, Column, ColumnAdd uint32 }

type Visual struct {
	Start, End       Position
	Mode, CursorWant uint32
}

type Entry struct {
	Top, Bottom, LineCount uint32
	Lines                  []string
}

// ReadFile parses a persistent undo file from disk.
func ReadFile(name string) (*File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse parses a Vim version 2, unencrypted persistent undo file.
func Parse(r io.Reader) (*File, error) {
	d := decoder{r: bufio.NewReader(r)}
	magic, err := d.bytes(len(startMagic))
	if err != nil {
		return nil, fmt.Errorf("read undo magic: %w", err)
	}
	if string(magic) != string(startMagic) {
		return nil, ErrNotUndoFile
	}
	fileVersion, err := d.u16()
	if err != nil {
		return nil, err
	}
	if fileVersion == encryptedVersion {
		return nil, ErrEncrypted
	}
	if fileVersion != version && fileVersion != 3 {
		return nil, fmt.Errorf("unsupported Vim undo version %d", fileVersion)
	}

	result := &File{Version: fileVersion}
	hash, err := d.bytes(len(result.Hash))
	if err != nil {
		return nil, err
	}
	copy(result.Hash[:], hash)
	if result.LineCount, err = d.u32(); err != nil {
		return nil, err
	}
	if result.SavedLine, err = d.string(); err != nil {
		return nil, err
	}
	if result.SavedLineNumber, err = d.u32(); err != nil {
		return nil, err
	}
	if result.SavedLineColumn, err = d.u32(); err != nil {
		return nil, err
	}
	for _, target := range []*uint32{&result.OldHead, &result.NewHead, &result.CurrentHead} {
		if *target, err = d.u32(); err != nil {
			return nil, err
		}
	}
	var headerCount uint32
	for _, target := range []*uint32{&headerCount, &result.SequenceLast, &result.SequenceCurrent} {
		if *target, err = d.u32(); err != nil {
			return nil, err
		}
	}
	if result.Time, err = d.timestamp(); err != nil {
		return nil, err
	}
	if headerCount > maxItems {
		return nil, fmt.Errorf("invalid undo header count %d", headerCount)
	}
	options, err := d.options()
	if err != nil {
		return nil, err
	}
	result.LastSaveNumber = optionUint32(options, 1)

	result.Headers = make([]Header, 0, headerCount)
	for {
		marker, err := d.u16()
		if err != nil {
			return nil, fmt.Errorf("read undo header marker: %w", err)
		}
		if marker == headerEndMagic {
			break
		}
		if marker != headerMagic {
			return nil, fmt.Errorf("invalid undo header marker %#x", marker)
		}
		header, err := d.header(fileVersion)
		if err != nil {
			return nil, err
		}
		result.Headers = append(result.Headers, header)
	}
	if uint32(len(result.Headers)) != headerCount {
		return nil, fmt.Errorf("undo header count is %d, read %d", headerCount, len(result.Headers))
	}
	return result, nil
}

type decoder struct{ r io.Reader }

func (d decoder) bytes(length int) ([]byte, error) {
	if length < 0 || length > maxStringLength {
		return nil, fmt.Errorf("invalid string length %d", length)
	}
	b := make([]byte, length)
	_, err := io.ReadFull(d.r, b)
	return b, err
}

func (d decoder) u8() (byte, error) {
	b, err := d.bytes(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (d decoder) u16() (uint16, error) {
	b, err := d.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (d decoder) u32() (uint32, error) {
	b, err := d.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (d decoder) timestamp() (time.Time, error) {
	b, err := d.bytes(8)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(binary.BigEndian.Uint64(b)), 0), nil
}

func (d decoder) string() (string, error) {
	length, err := d.u32()
	if err != nil {
		return "", err
	}
	b, err := d.bytes(int(length))
	return string(b), err
}

func (d decoder) position() (Position, error) {
	var p Position
	var err error
	for _, target := range []*uint32{&p.Line, &p.Column, &p.ColumnAdd} {
		if *target, err = d.u32(); err != nil {
			return Position{}, err
		}
	}
	return p, nil
}

func (d decoder) options() (map[byte][]byte, error) {
	options := map[byte][]byte{}
	for {
		length, err := d.u8()
		if err != nil {
			return nil, err
		}
		if length == 0 {
			return options, nil
		}
		kind, err := d.u8()
		if err != nil {
			return nil, err
		}
		value, err := d.bytes(int(length))
		if err != nil {
			return nil, err
		}
		options[kind] = value
	}
}

func optionUint32(options map[byte][]byte, kind byte) uint32 {
	if value := options[kind]; len(value) == 4 {
		return binary.BigEndian.Uint32(value)
	}
	return 0
}

func (d decoder) header(fileVersion uint16) (Header, error) {
	var h Header
	var err error
	for _, target := range []*uint32{&h.Next, &h.Previous, &h.AltNext, &h.AltPrevious, &h.Sequence} {
		if *target, err = d.u32(); err != nil {
			return Header{}, err
		}
	}
	if h.Cursor, err = d.position(); err != nil {
		return Header{}, err
	}
	vcol, err := d.u32()
	if err != nil {
		return Header{}, err
	}
	h.CursorVirtualColumn = int32(vcol)
	if h.Flags, err = d.u16(); err != nil {
		return Header{}, err
	}
	for i := range h.Marks {
		if h.Marks[i], err = d.position(); err != nil {
			return Header{}, err
		}
	}
	if h.Visual.Start, err = d.position(); err != nil {
		return Header{}, err
	}
	if h.Visual.End, err = d.position(); err != nil {
		return Header{}, err
	}
	if h.Visual.Mode, err = d.u32(); err != nil {
		return Header{}, err
	}
	if h.Visual.CursorWant, err = d.u32(); err != nil {
		return Header{}, err
	}
	if h.Time, err = d.timestamp(); err != nil {
		return Header{}, err
	}
	options, err := d.options()
	if err != nil {
		return Header{}, err
	}
	h.SaveNumber = optionUint32(options, 1)

	for {
		marker, err := d.u16()
		if err != nil {
			return Header{}, err
		}
		if marker == entryEndMagic {
			if fileVersion != 3 {
				return h, nil
			}
			return d.extmarks(h)
		}
		if marker != entryMagic {
			return Header{}, fmt.Errorf("invalid undo entry marker %#x", marker)
		}
		entry, err := d.entry()
		if err != nil {
			return Header{}, err
		}
		h.Entries = append(h.Entries, entry)
	}
}

func (d decoder) extmarks(header Header) (Header, error) {
	for {
		marker, err := d.u16()
		if err != nil {
			return Header{}, err
		}
		if marker == entryEndMagic {
			return header, nil
		}
		if marker != entryMagic {
			return Header{}, fmt.Errorf("invalid extmark marker %#x", marker)
		}
		kind, err := d.u32()
		if err != nil {
			return Header{}, err
		}
		if kind > 1 { // Neovim only serializes splice (0) and move (1) extmarks.
			return Header{}, ErrExtmarks
		}
		if _, err := d.bytes(extmarkDataLength); err != nil {
			return Header{}, err
		}
	}
}

func (d decoder) entry() (Entry, error) {
	var entry Entry
	var err error
	var count uint32
	for _, target := range []*uint32{&entry.Top, &entry.Bottom, &entry.LineCount, &count} {
		if *target, err = d.u32(); err != nil {
			return Entry{}, err
		}
	}
	if count > maxItems {
		return Entry{}, fmt.Errorf("invalid undo entry line count %d", count)
	}
	entry.Lines = make([]string, 0, count)
	for range count {
		line, err := d.string()
		if err != nil {
			return Entry{}, err
		}
		entry.Lines = append(entry.Lines, line)
	}
	return entry, nil
}
