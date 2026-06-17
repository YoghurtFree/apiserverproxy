package config

import (
	"context"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

// Watcher watches a config file and triggers a callback on changes.
type Watcher struct {
	path     string
	onChange func(*ClustersConfig)
	mu       sync.Mutex
	timer    *time.Timer
}

// NewWatcher creates a file watcher that calls onChange when the config changes.
func NewWatcher(path string, onChange func(*ClustersConfig)) *Watcher {
	return &Watcher{
		path:     path,
		onChange: onChange,
	}
}

// Start begins watching the config file. Blocks until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	if err := fw.Add(w.path); err != nil {
		return err
	}

	log.Infof("watching config file: %s", w.path)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Write == 0 {
				continue
			}
			w.debounce()
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			log.Errorf("config watcher error: %v", err)
		}
	}
}

// debounce coalesces multiple rapid file changes into a single reload.
func (w *Watcher) debounce() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(500*time.Millisecond, func() {
		cfg, err := LoadClustersConfig(w.path)
		if err != nil {
			log.Warnf("config reload failed: %v", err)
			return
		}
		log.Infof("config reloaded, %d clusters", len(cfg.Clusters))
		w.onChange(cfg)
	})
}
