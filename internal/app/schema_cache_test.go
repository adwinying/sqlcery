package app

import "testing"

func TestAutocompleteSchemaCacheClonesStoredAndSnapshotData(t *testing.T) {
	cache := newAutocompleteSchemaCache()
	original := &AutocompleteSchemaContext{
		Tables: []AutocompleteTableContext{{Namespace: "main", Name: "users", Columns: []string{"id", "name"}}},
	}

	cache.Replace(original)
	original.Tables[0].Name = "mutated"
	original.Tables[0].Columns[0] = "mutated"

	snapshot := cache.Snapshot()
	if snapshot == nil {
		t.Fatal("Snapshot() = nil, want schema")
	}

	snapshot.Tables[0].Name = "changed"
	snapshot.Tables[0].Columns[1] = "changed"

	again := cache.Snapshot()
	if again == nil {
		t.Fatal("Snapshot() = nil, want cloned schema")
	}

	if got, want := again.Tables[0].Name, "users"; got != want {
		t.Fatalf("again.Tables[0].Name = %q, want %q", got, want)
	}

	if got, want := again.Tables[0].Columns[0], "id"; got != want {
		t.Fatalf("again.Tables[0].Columns[0] = %q, want %q", got, want)
	}

	if got, want := again.Tables[0].Columns[1], "name"; got != want {
		t.Fatalf("again.Tables[0].Columns[1] = %q, want %q", got, want)
	}
}
