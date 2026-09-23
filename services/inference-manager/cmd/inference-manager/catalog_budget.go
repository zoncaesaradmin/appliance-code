package main

// ollamaMemoryEstimate budgets the actual quantized model layer plus a
// proportional working set and fixed context/runtime headroom. Doubling the
// layer falsely excludes MXFP4 models such as gpt-oss:20b on 32 GiB hosts.
// This remains an estimate: Ollama's context and parallelism can increase the
// real peak, so the engine's cgroup limit and load-time checks still apply.
func ollamaMemoryEstimate(weights uint64) uint64 {
	if weights == 0 {
		return 0
	}
	working := weights / 5
	if working < 2<<30 {
		working = 2 << 30
	}
	return weights + working + (1 << 30)
}

// combineCatalogMemory merges host MemAvailable with an optional GPU free-memory
// probe into the catalog eligibility budget. gpuFree may be 0 when the thin
// manager cannot see CUDA (Restricted CPU pod); that must not wipe host capacity.
func combineCatalogMemory(hostAvailable, gpuFree uint64, usingGPU bool, engine string) uint64 {
	if engine == "vllm" && !usingGPU {
		return 0
	}
	memory := hostAvailable / 4 * 3
	if usingGPU && gpuFree > 0 {
		gpu := gpuFree / 5 * 4
		if memory == 0 || gpu < memory {
			memory = gpu
		}
	}
	return memory
}
