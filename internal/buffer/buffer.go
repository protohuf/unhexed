package buffer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type Operation struct {
	Type    OpType
	Offset  int64
	OldData []byte
	NewData []byte
}

type OpType int

const (
	OpInsert OpType = iota
	OpDelete
	OpReplace
)

type Buffer struct {
	filename     string
	data         []byte
	originalHash string
	modified     bool
	undoStack    []Operation
	redoStack    []Operation
	isNew        bool
}

func New() *Buffer {
	return &Buffer{
		filename: "",
		data:     make([]byte, 0),
		modified: false,
		isNew:    true,
	}
}

func Open(filename string) (*Buffer, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(data)

	return &Buffer{
		filename:     filename,
		data:         data,
		originalHash: hex.EncodeToString(hash[:]),
		modified:     false,
		isNew:        false,
	}, nil
}

func (b *Buffer) Filename() string {
	return b.filename
}

func (b *Buffer) SetFilename(name string) {
	b.filename = name
	b.isNew = false
}

func (b *Buffer) IsNew() bool {
	return b.isNew
}

func (b *Buffer) IsModified() bool {
	return b.modified
}

func (b *Buffer) Size() int64 {
	return int64(len(b.data))
}

func (b *Buffer) Data() []byte {
	return b.data
}

func (b *Buffer) GetByte(offset int64) (byte, bool) {
	if offset < 0 || offset >= int64(len(b.data)) {
		return 0, false
	}
	return b.data[offset], true
}

func (b *Buffer) GetBytes(offset int64, count int) []byte {
	if offset < 0 || offset >= int64(len(b.data)) {
		return nil
	}
	end := offset + int64(count)
	if end > int64(len(b.data)) {
		end = int64(len(b.data))
	}
	result := make([]byte, end-offset)
	copy(result, b.data[offset:end])
	return result
}

func (b *Buffer) Insert(offset int64, data []byte) {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(b.data)) {
		offset = int64(len(b.data))
	}

	op := Operation{
		Type:    OpInsert,
		Offset:  offset,
		NewData: make([]byte, len(data)),
	}
	copy(op.NewData, data)
	b.undoStack = append(b.undoStack, op)
	b.redoStack = nil

	newData := make([]byte, len(b.data)+len(data))
	copy(newData, b.data[:offset])
	copy(newData[offset:], data)
	copy(newData[offset+int64(len(data)):], b.data[offset:])
	b.data = newData
	b.modified = true
}

func (b *Buffer) Delete(offset int64, count int) {
	if offset < 0 || offset >= int64(len(b.data)) || count <= 0 {
		return
	}
	if offset+int64(count) > int64(len(b.data)) {
		count = int(int64(len(b.data)) - offset)
	}

	op := Operation{
		Type:    OpDelete,
		Offset:  offset,
		OldData: make([]byte, count),
	}
	copy(op.OldData, b.data[offset:offset+int64(count)])
	b.undoStack = append(b.undoStack, op)
	b.redoStack = nil

	newData := make([]byte, len(b.data)-count)
	copy(newData, b.data[:offset])
	copy(newData[offset:], b.data[offset+int64(count):])
	b.data = newData
	b.modified = true
}

func (b *Buffer) Replace(offset int64, newByte byte) {
	if offset < 0 || offset >= int64(len(b.data)) {
		return
	}

	op := Operation{
		Type:    OpReplace,
		Offset:  offset,
		OldData: []byte{b.data[offset]},
		NewData: []byte{newByte},
	}
	b.undoStack = append(b.undoStack, op)
	b.redoStack = nil

	b.data[offset] = newByte
	b.modified = true
}

func (b *Buffer) ReplaceBytes(offset int64, data []byte) {
	for i, d := range data {
		pos := offset + int64(i)
		if pos >= int64(len(b.data)) {
			// Extend file
			b.Insert(int64(len(b.data)), []byte{d})
		} else {
			b.Replace(pos, d)
		}
	}
}

func (b *Buffer) Undo() bool {
	if len(b.undoStack) == 0 {
		return false
	}

	op := b.undoStack[len(b.undoStack)-1]
	b.undoStack = b.undoStack[:len(b.undoStack)-1]

	switch op.Type {
	case OpInsert:
		// Undo insert = delete
		newData := make([]byte, len(b.data)-len(op.NewData))
		copy(newData, b.data[:op.Offset])
		copy(newData[op.Offset:], b.data[op.Offset+int64(len(op.NewData)):])
		b.data = newData
	case OpDelete:
		// Undo delete = insert
		newData := make([]byte, len(b.data)+len(op.OldData))
		copy(newData, b.data[:op.Offset])
		copy(newData[op.Offset:], op.OldData)
		copy(newData[op.Offset+int64(len(op.OldData)):], b.data[op.Offset:])
		b.data = newData
	case OpReplace:
		// Undo replace = restore old byte
		b.data[op.Offset] = op.OldData[0]
	}

	b.redoStack = append(b.redoStack, op)
	b.modified = len(b.undoStack) > 0
	return true
}

func (b *Buffer) Redo() bool {
	if len(b.redoStack) == 0 {
		return false
	}

	op := b.redoStack[len(b.redoStack)-1]
	b.redoStack = b.redoStack[:len(b.redoStack)-1]

	switch op.Type {
	case OpInsert:
		newData := make([]byte, len(b.data)+len(op.NewData))
		copy(newData, b.data[:op.Offset])
		copy(newData[op.Offset:], op.NewData)
		copy(newData[op.Offset+int64(len(op.NewData)):], b.data[op.Offset:])
		b.data = newData
	case OpDelete:
		newData := make([]byte, len(b.data)-len(op.OldData))
		copy(newData, b.data[:op.Offset])
		copy(newData[op.Offset:], b.data[op.Offset+int64(len(op.OldData)):])
		b.data = newData
	case OpReplace:
		b.data[op.Offset] = op.NewData[0]
	}

	b.undoStack = append(b.undoStack, op)
	b.modified = true
	return true
}

func (b *Buffer) CanUndo() bool {
	return len(b.undoStack) > 0
}

func (b *Buffer) CanRedo() bool {
	return len(b.redoStack) > 0
}

func (b *Buffer) HasChangedOnDisk() (bool, error) {
	if b.isNew || b.filename == "" {
		return false, nil
	}

	f, err := os.Open(b.filename)
	if err != nil {
		return false, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return false, err
	}

	hash := sha256.Sum256(data)
	currentHash := hex.EncodeToString(hash[:])

	return currentHash != b.originalHash, nil
}

func (b *Buffer) Save() error {
	if b.filename == "" {
		return fmt.Errorf("no filename set")
	}

	if err := os.WriteFile(b.filename, b.data, 0644); err != nil {
		return err
	}

	// Update hash
	hash := sha256.Sum256(b.data)
	b.originalHash = hex.EncodeToString(hash[:])
	b.modified = false
	b.undoStack = nil
	b.redoStack = nil
	b.isNew = false

	return nil
}

func (b *Buffer) SaveAs(filename string) error {
	b.filename = filename
	return b.Save()
}

func (b *Buffer) Find(pattern []byte, startOffset int64, forward bool) int64 {
	if len(pattern) == 0 || len(b.data) == 0 {
		return -1
	}

	if forward {
		for i := startOffset; i <= int64(len(b.data))-int64(len(pattern)); i++ {
			match := true
			for j := 0; j < len(pattern); j++ {
				if b.data[i+int64(j)] != pattern[j] {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
	} else {
		start := startOffset - 1
		if start > int64(len(b.data))-int64(len(pattern)) {
			start = int64(len(b.data)) - int64(len(pattern))
		}
		for i := start; i >= 0; i-- {
			match := true
			for j := 0; j < len(pattern); j++ {
				if b.data[i+int64(j)] != pattern[j] {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
	}

	return -1
}

func (b *Buffer) CountMatches(pattern []byte) int {
	if len(pattern) == 0 || len(b.data) == 0 {
		return 0
	}

	count := 0
	for i := int64(0); i <= int64(len(b.data))-int64(len(pattern)); i++ {
		match := true
		for j := 0; j < len(pattern); j++ {
			if b.data[i+int64(j)] != pattern[j] {
				match = false
				break
			}
		}
		if match {
			count++
		}
	}
	return count
}
