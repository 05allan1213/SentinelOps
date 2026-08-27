package mysql

import (
	"context"
	"testing"
)

func TestCreateEventNormalizesEmptyMetadataBeforeDatabaseAccess(t *testing.T) {
	event := &Event{Metadata: " \t\n"}
	if err := CreateEvent(context.Background(), event); err == nil {
		t.Fatal("CreateEvent() unexpectedly succeeded without an initialized database")
	}
	if event.Metadata != `{}` {
		t.Fatalf("CreateEvent() metadata = %q, want valid empty JSON object", event.Metadata)
	}
}
