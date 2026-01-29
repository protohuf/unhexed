package buffer

import (
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	b := New()
	if b.Size() != 0 {
		t.Errorf("expected size 0, got %d", b.Size())
	}
	if !b.IsNew() {
		t.Error("expected IsNew to be true")
	}
}

func TestInsert(t *testing.T) {
	b := New()
	b.Insert(0, []byte{0x41, 0x42, 0x43})

	if b.Size() != 3 {
		t.Errorf("expected size 3, got %d", b.Size())
	}

	if val, ok := b.GetByte(0); !ok || val != 0x41 {
		t.Errorf("expected 0x41 at offset 0, got %02X", val)
	}
	if val, ok := b.GetByte(1); !ok || val != 0x42 {
		t.Errorf("expected 0x42 at offset 1, got %02X", val)
	}
	if val, ok := b.GetByte(2); !ok || val != 0x43 {
		t.Errorf("expected 0x43 at offset 2, got %02X", val)
	}
}

func TestDelete(t *testing.T) {
	b := New()
	b.Insert(0, []byte{0x41, 0x42, 0x43, 0x44})
	b.Delete(1, 2) // Delete 0x42 and 0x43

	if b.Size() != 2 {
		t.Errorf("expected size 2, got %d", b.Size())
	}

	if val, ok := b.GetByte(0); !ok || val != 0x41 {
		t.Errorf("expected 0x41 at offset 0, got %02X", val)
	}
	if val, ok := b.GetByte(1); !ok || val != 0x44 {
		t.Errorf("expected 0x44 at offset 1, got %02X", val)
	}
}

func TestReplace(t *testing.T) {
	b := New()
	b.Insert(0, []byte{0x41, 0x42, 0x43})
	b.Replace(1, 0xFF)

	if val, ok := b.GetByte(1); !ok || val != 0xFF {
		t.Errorf("expected 0xFF at offset 1, got %02X", val)
	}
}

func TestUndo(t *testing.T) {
	b := New()
	b.Insert(0, []byte{0x41})

	if !b.CanUndo() {
		t.Error("expected CanUndo to be true")
	}

	b.Undo()

	if b.Size() != 0 {
		t.Errorf("expected size 0 after undo, got %d", b.Size())
	}
}

func TestRedo(t *testing.T) {
	b := New()
	b.Insert(0, []byte{0x41})
	b.Undo()

	if !b.CanRedo() {
		t.Error("expected CanRedo to be true")
	}

	b.Redo()

	if b.Size() != 1 {
		t.Errorf("expected size 1 after redo, got %d", b.Size())
	}
}

func TestFind(t *testing.T) {
	b := New()
	b.Insert(0, []byte("Hello, World!"))

	pos := b.Find([]byte("World"), 0, true)
	if pos != 7 {
		t.Errorf("expected position 7, got %d", pos)
	}

	pos = b.Find([]byte("xyz"), 0, true)
	if pos != -1 {
		t.Errorf("expected -1 for not found, got %d", pos)
	}
}

func TestOpenAndSave(t *testing.T) {
	// Create temp file
	f, err := os.CreateTemp("", "unhexed_test_*.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	testData := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	f.Write(testData)
	f.Close()

	// Open file
	b, err := Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	if b.Size() != 5 {
		t.Errorf("expected size 5, got %d", b.Size())
	}

	// Modify and save
	b.Replace(2, 0xFF)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify
	b2, err := Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	if val, ok := b2.GetByte(2); !ok || val != 0xFF {
		t.Errorf("expected 0xFF at offset 2, got %02X", val)
	}
}

func TestGetBytes(t *testing.T) {
	b := New()
	b.Insert(0, []byte{0x01, 0x02, 0x03, 0x04, 0x05})

	bytes := b.GetBytes(1, 3)
	if len(bytes) != 3 {
		t.Errorf("expected 3 bytes, got %d", len(bytes))
	}
	if bytes[0] != 0x02 || bytes[1] != 0x03 || bytes[2] != 0x04 {
		t.Errorf("unexpected bytes: %v", bytes)
	}
}

func TestCountMatches(t *testing.T) {
	b := New()
	b.Insert(0, []byte("ababab"))

	count := b.CountMatches([]byte("ab"))
	if count != 3 {
		t.Errorf("expected 3 matches, got %d", count)
	}
}
