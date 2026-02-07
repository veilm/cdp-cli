package cdp

import (
	"context"
	"testing"
)

func TestRemoteObjectValue_NullSubtype(t *testing.T) {
	c := &Client{}
	v, err := c.RemoteObjectValue(context.Background(), RemoteObject{
		Type:    "object",
		Subtype: "null",
		// Value/Description may be omitted by the protocol for null.
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if v != nil {
		t.Fatalf("expected nil value, got %#v", v)
	}
}
