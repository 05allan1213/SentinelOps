package indexer

import (
	"errors"
	"testing"
)

func TestDerivedEffectPartialIndexerFailureIsReturned(t *testing.T) {
	partial := errors.New("second Milvus batch failed")
	ids, err := finalizeStoreResult([]string{"event-p24-1"}, 2, []error{partial})
	if len(ids) != 1 || !errors.Is(err, partial) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	ids, err = finalizeStoreResult([]string{"event-p24-1"}, 2, nil)
	if len(ids) != 1 || err == nil {
		t.Fatalf("silent partial result ids=%v err=%v", ids, err)
	}
	ids, err = finalizeStoreResult([]string{"event-p24-1", "event-p24-2"}, 2, nil)
	if len(ids) != 2 || err != nil {
		t.Fatalf("complete result ids=%v err=%v", ids, err)
	}
}
