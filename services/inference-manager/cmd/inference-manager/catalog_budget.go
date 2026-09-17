package main

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
