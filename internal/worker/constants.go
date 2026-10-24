package worker

import "github.com/memory-engine/memory-engine/internal/domain"

// staleStatus is a package-level var used by decay_worker.
// Declared here to avoid re-typing the constant.
var staleStatus = domain.MemoryStatusStale
