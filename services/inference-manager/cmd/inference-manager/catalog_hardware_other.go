//go:build !linux

package main

import "context"

func (m *manager) catalogBudget(context.Context) (uint64, uint64) { return 0, 0 }
