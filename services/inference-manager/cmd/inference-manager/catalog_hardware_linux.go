//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func hostMemAvailable() uint64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			n, _ := strconv.ParseUint(fields[1], 10, 64)
			return n * 1024
		}
	}
	return 0
}

// probeGPUFreeMemoryBytes reports free memory on GPU 0 for catalog eligibility.
// The thin inference-manager pod is Restricted/CPU-only: torch/CUDA and often
// nvidia-smi are unavailable there even when INFERENCE_GPU_ENABLED=true and the
// on-demand engine pod will have the GPU. Callers must fall back to host
// MemAvailable rather than treating a probe miss as zero capacity.
func probeGPUFreeMemoryBytes(ctx context.Context) uint64 {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	python := env("INFERENCE_PYTHON", "python3")
	if b, err := exec.CommandContext(probeCtx, python, "-c", "import torch; print(torch.cuda.mem_get_info(0)[0])").Output(); err == nil {
		n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		if n > 0 {
			return n
		}
	}
	if b, err := exec.CommandContext(probeCtx, "nvidia-smi", "--query-gpu=memory.free", "--format=csv,noheader,nounits").Output(); err == nil {
		line := strings.TrimSpace(strings.Split(string(b), "\n")[0])
		if mib, err := strconv.ParseUint(line, 10, 64); err == nil && mib > 0 {
			return mib * 1024 * 1024
		}
	}
	return 0
}

// gpuFreeMemoryProbe is overridable in tests.
var gpuFreeMemoryProbe = probeGPUFreeMemoryBytes

func (m *manager) catalogBudget(ctx context.Context) (uint64, uint64) {
	var disk syscall.Statfs_t
	if syscall.Statfs(m.modelsDir, &disk) != nil {
		return 0, 0
	}
	freeDisk := disk.Bavail * uint64(disk.Bsize)
	usingGPU, _ := m.resolveDevice(ctx)
	gpuFree := uint64(0)
	if usingGPU {
		gpuFree = gpuFreeMemoryProbe(ctx)
	}
	memory := combineCatalogMemory(hostMemAvailable(), gpuFree, usingGPU, m.engine)
	if max := packageMaxMemoryBytes(); max > 0 && memory > max {
		memory = max
	}
	return memory, freeDisk
}
