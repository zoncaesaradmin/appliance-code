package main

import "testing"

func TestNormalizeCatalogSortDefaults(t *testing.T) {
	sortBy, order, err := normalizeCatalogSort("", "")
	if err != nil || sortBy != "parameters" || order != "desc" {
		t.Fatalf("got sort=%q order=%q err=%v", sortBy, order, err)
	}
	sortBy, order, err = normalizeCatalogSort("name", "")
	if err != nil || sortBy != "name" || order != "asc" {
		t.Fatalf("name default order: sort=%q order=%q err=%v", sortBy, order, err)
	}
	if _, _, err := normalizeCatalogSort("downloads", "desc"); err == nil {
		t.Fatal("expected unsupported sort")
	}
}

func TestSortCatalogItemsByParametersDescending(t *testing.T) {
	items := []catalogEntry{
		{ID: "org/tiny-0.5B", DownloadBytes: 1},
		{ID: "org/large-7B", DownloadBytes: 1},
		{ID: "org/mid-3B", DownloadBytes: 1},
		{ID: "org/unknown", DownloadBytes: 100},
	}
	sortCatalogItems(items, "parameters", "desc")
	want := []string{"org/large-7B", "org/mid-3B", "org/tiny-0.5B", "org/unknown"}
	for i, id := range want {
		if items[i].ID != id {
			t.Fatalf("index %d: got %q want %q (%v)", i, items[i].ID, id, items)
		}
	}
}

func TestSortCatalogItemsByMemoryAscending(t *testing.T) {
	items := []catalogEntry{
		{ID: "b", MemoryBytes: 300},
		{ID: "a", MemoryBytes: 100},
		{ID: "c", MemoryBytes: 200},
	}
	sortCatalogItems(items, "memory", "asc")
	if items[0].ID != "a" || items[1].ID != "c" || items[2].ID != "b" {
		t.Fatalf("unexpected order: %+v", items)
	}
}
