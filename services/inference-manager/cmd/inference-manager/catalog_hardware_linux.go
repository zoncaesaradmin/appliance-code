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

func (m *manager) catalogBudget(ctx context.Context) (uint64, uint64) {
	var disk syscall.Statfs_t
	if syscall.Statfs(m.modelsDir, &disk) != nil {
		return 0, 0
	}
	freeDisk := disk.Bavail * uint64(disk.Bsize)
	_, mode, _ := m.selectMode(ctx)
	if mode == "" {
		return 0, freeDisk
	}
	// Catalog eligibility estimates whether a *model* can fit on this appliance.
	// Do not clamp to the manager container's memory limit: the thin manager is
	// intentionally small (API/proxy only); the engine sidecar / host holds the
	// model. Host MemAvailable is the conservative capacity signal for CPU mode.
	memory := hostMemAvailable()
	// Leave room for existing appliance workloads and transient allocations.
	memory = memory / 4 * 3
	if mode == "cuda" {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		// Conservative single-device capacity: do not add GPU memories together
		// unless a distributed/tensor-parallel launch has been configured.
		b, err := exec.CommandContext(probeCtx, env("INFERENCE_PYTHON", "python3"), "-c", "import torch; print(torch.cuda.mem_get_info(0)[0])").Output()
		if err != nil {
			return 0, freeDisk
		}
		gpu, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		gpu = gpu / 5 * 4
		if memory == 0 || gpu < memory {
			memory = gpu
		}
	}
	return memory, freeDisk
}
