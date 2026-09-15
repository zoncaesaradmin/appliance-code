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

func readMemoryValue(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return n
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
	var memory uint64
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "MemAvailable:" {
				memory, _ = strconv.ParseUint(fields[1], 10, 64)
				memory *= 1024
			}
		}
	}
	for _, paths := range [][2]string{{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory.current"}, {"/sys/fs/cgroup/memory/memory.limit_in_bytes", "/sys/fs/cgroup/memory/memory.usage_in_bytes"}} {
		limit, used := readMemoryValue(paths[0]), readMemoryValue(paths[1])
		if limit > 0 {
			if used >= limit {
				return 0, freeDisk
			}
			if limit-used < memory {
				memory = limit - used
			}
		}
	}
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
		if gpu < memory {
			memory = gpu
		}
	}
	return memory, freeDisk
}
