package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// Archiving is the safe alternative to deleting: the chat leaves the inbox but
// every message, order and audit row stays.
func TestArchiveHidesChatWithoutLosingIt(t *testing.T) {
	st, err := OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	const phone = "573001112233"

	c, err := st.EnsureContact(ctx, phone, "Ana", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Archived {
		t.Fatal("a new contact must not start archived")
	}
	if _, err := st.AddMessage(ctx, Message{Phone: phone, Direction: DirIn, Sender: SenderCustomer, Body: "hola"}); err != nil {
		t.Fatal(err)
	}

	if err := st.SetArchived(ctx, phone, true); err != nil {
		t.Fatal(err)
	}
	inbox, err := st.ListContacts(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 0 {
		t.Fatalf("archived chat still in the inbox: %d", len(inbox))
	}
	arch, err := st.ListContacts(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(arch) != 1 || !arch[0].Archived {
		t.Fatalf("archive should list the chat, got %+v", arch)
	}
	if arch[0].LastPreview != "hola" {
		t.Fatalf("preview lost on the way to the archive: %q", arch[0].LastPreview)
	}
	ms, err := st.Messages(ctx, phone, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("archiving must not delete messages, got %d", len(ms))
	}

	if err := st.SetArchived(ctx, phone, false); err != nil {
		t.Fatal(err)
	}
	if inbox, _ = st.ListContacts(ctx, false); len(inbox) != 1 {
		t.Fatalf("unarchived chat did not return to the inbox: %d", len(inbox))
	}

	if err := st.SetArchived(ctx, "573009999999", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archiving an unknown phone should be ErrNotFound, got %v", err)
	}
}

// A database made by an older Motor has no archived column: opening it must
// add one, not fail nor lose the chats.
func TestMigrationAddsArchiveToOldDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "viejo.db")
	const phone = "573004445566"

	st, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureContact(ctx, phone, "Viejo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`ALTER TABLE contacts DROP COLUMN archived`); err != nil {
		t.Skipf("este SQLite no permite quitar columnas: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := OpenPath(path)
	if err != nil {
		t.Fatalf("reabrir una base sin la columna archived: %v", err)
	}
	defer st2.Close()
	c, err := st2.GetContact(ctx, phone)
	if err != nil {
		t.Fatal(err)
	}
	if c.Archived {
		t.Fatal("the added column must default to 0")
	}
	inbox, err := st2.ListContacts(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 || inbox[0].Phone != phone {
		t.Fatalf("the old chat was lost in the migration: %+v", inbox)
	}
}
