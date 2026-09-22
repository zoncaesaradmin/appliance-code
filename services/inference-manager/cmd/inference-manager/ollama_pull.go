package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type ollamaPullEvent struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     uint64 `json:"total"`
	Completed uint64 `json:"completed"`
	Error     string `json:"error"`
}

type ollamaPullLayer struct{ completed, total uint64 }

// pullOllamaModel consumes Ollama's newline-delimited progress stream. The
// timer covers both a stalled connection and a pull that stops making useful
// progress after the connection opens.
func (m *manager) pullOllamaModel(ctx context.Context, source string, idleTimeout time.Duration) error {
	pullCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	timer := time.AfterFunc(idleTimeout, func() {
		timedOut.Store(true)
		cancel()
	})
	defer timer.Stop()
	requestURL := m.backend.ResolveReference(&url.URL{Path: "/api/pull"})
	body, err := json.Marshal(map[string]any{"model": source, "stream": true})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(pullCtx, http.MethodPost, requestURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ollamaPullError(err, timedOut.Load())
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Ollama pull returned %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}

	layers := make(map[string]ollamaPullLayer)
	lastStatus := ""
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event ollamaPullEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode Ollama pull progress: %w", err)
		}
		if event.Error != "" {
			return fmt.Errorf("Ollama pull: %s", event.Error)
		}
		if event.Status == "success" {
			return nil
		}
		key := event.Digest
		if key == "" {
			key = "current"
		}
		layer := layers[key]
		advanced := event.Completed > layer.completed || (event.Status != "" && event.Status != lastStatus)
		if event.Completed > layer.completed {
			layer.completed = event.Completed
		}
		if event.Total > layer.total {
			layer.total = event.Total
		}
		layers[key] = layer
		var completed, total uint64
		for _, current := range layers {
			completed += current.completed
			total += current.total
		}
		if event.Status != "" {
			lastStatus = event.Status
		}
		m.updateOllamaPullProgress(completed, total, lastStatus)
		if advanced {
			timer.Reset(idleTimeout)
		}
	}
	if err := scanner.Err(); err != nil {
		return ollamaPullError(err, timedOut.Load())
	}
	if timedOut.Load() {
		return ollamaPullError(context.Canceled, true)
	}
	return errors.New("Ollama pull ended without a success event")
}

func ollamaPullError(err error, timedOut bool) error {
	if timedOut {
		return errors.New("Ollama pull made no progress for two minutes; check registry connectivity and retry")
	}
	return fmt.Errorf("Ollama pull: %w", err)
}

func (m *manager) updateOllamaPullProgress(completed, observedTotal uint64, status string) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	if m.progress.State != "downloading" {
		return
	}
	// A catalog import has an aggregate size estimate and starts with a
	// percentage. For an explicit import Ollama only reveals one layer at a
	// time, so keep the percentage indeterminate as new layers appear.
	hasAggregateTotal := m.progress.Percent != nil
	if completed > m.progress.BytesDownloaded {
		m.progress.BytesDownloaded = completed
	}
	if m.progress.BytesTotal == 0 && observedTotal > 0 {
		m.progress.BytesTotal = observedTotal
	}
	if hasAggregateTotal && m.progress.BytesTotal > 0 {
		percent := int(m.progress.BytesDownloaded * 100 / m.progress.BytesTotal)
		if percent > 99 {
			percent = 99 // The final success event is still required.
		}
		m.progress.Percent = &percent
	}
	if status != "" {
		m.progress.Message = status
	}
	m.progress.UpdatedAt = time.Now().UTC()
	_ = m.writeProgressFileLocked()
}
