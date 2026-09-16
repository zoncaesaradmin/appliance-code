package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var catalogParamHintRE = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*([mb])(?:[-_]|$)`)

// normalizeCatalogSort validates query values. Default is parameters descending
// (largest / highest-parameter candidates first).
func normalizeCatalogSort(sortBy, order string) (string, string, error) {
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	order = strings.ToLower(strings.TrimSpace(order))
	if sortBy == "" {
		sortBy = "parameters"
	}
	switch sortBy {
	case "parameters", "memory", "name":
	default:
		return "", "", errUnsupportedCatalogSort(sortBy)
	}
	if order == "" {
		if sortBy == "name" {
			order = "asc"
		} else {
			order = "desc"
		}
	}
	if order != "asc" && order != "desc" {
		return "", "", errUnsupportedCatalogOrder(order)
	}
	return sortBy, order, nil
}

type catalogSortError struct{ message string }

func (e catalogSortError) Error() string { return e.message }

func errUnsupportedCatalogSort(value string) error {
	return catalogSortError{message: `unsupported sort "` + value + `"; use parameters, memory, or name`}
}

func errUnsupportedCatalogOrder(value string) error {
	return catalogSortError{message: `unsupported order "` + value + `"; use asc or desc`}
}

// catalogSizeRank estimates model scale for sorting: named parameter hint in
// the id, else downloadBytes/2 (FP16 stand-in), else memoryBytes.
func catalogSizeRank(entry catalogEntry) uint64 {
	if match := catalogParamHintRE.FindStringSubmatch(entry.ID); len(match) == 3 {
		amount, err := strconv.ParseFloat(match[1], 64)
		if err == nil && amount > 0 {
			switch strings.ToLower(match[2]) {
			case "b":
				return uint64(amount * 1e9)
			case "m":
				return uint64(amount * 1e6)
			}
		}
	}
	if entry.DownloadBytes > 0 {
		return entry.DownloadBytes / 2
	}
	return entry.MemoryBytes
}

func sortCatalogItems(items []catalogEntry, sortBy, order string) {
	descending := order == "desc"
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		var less bool
		switch sortBy {
		case "memory":
			if left.MemoryBytes != right.MemoryBytes {
				less = left.MemoryBytes < right.MemoryBytes
				if descending {
					return !less
				}
				return less
			}
		case "name":
			if left.ID != right.ID {
				less = left.ID < right.ID
				if descending {
					return !less
				}
				return less
			}
			return false
		default: // parameters
			leftRank, rightRank := catalogSizeRank(left), catalogSizeRank(right)
			if leftRank != rightRank {
				less = leftRank < rightRank
				if descending {
					return !less
				}
				return less
			}
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return false
	})
}
