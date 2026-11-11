package convo

import (
	"context"
	"testing"
)

// A chat in the archive must come back to the inbox as soon as the customer
// writes again, so an archived conversation never hides a live one.
func TestArchivedChatReturnsWhenCustomerWrites(t *testing.T) {
	r := newRig(t, manualNegocio())
	ctx := context.Background()
	const phone = "573001112233"

	if _, err := r.st.EnsureContact(ctx, phone, "Ana", false); err != nil {
		t.Fatal(err)
	}
	if err := r.st.SetArchived(ctx, phone, true); err != nil {
		t.Fatal(err)
	}
	if inbox, _ := r.st.ListContacts(ctx, false); len(inbox) != 0 {
		t.Fatalf("the chat should be out of the inbox, got %d", len(inbox))
	}

	if err := r.eng.HandleInbound(ctx, Inbound{Phone: phone, Text: "hola, sigo interesada"}); err != nil {
		t.Fatal(err)
	}
	c, err := r.st.GetContact(ctx, phone)
	if err != nil {
		t.Fatal(err)
	}
	if c.Archived {
		t.Fatal("a new customer message must bring the chat back to the inbox")
	}
	if inbox, _ := r.st.ListContacts(ctx, false); len(inbox) != 1 {
		t.Fatalf("chat not back in the inbox: %d", len(inbox))
	}
}
