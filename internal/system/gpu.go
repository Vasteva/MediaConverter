package system

import (
	"log"
	"os/exec"
)

// DetectGPU attempts to automatically identify the available GPU vendor
func DetectGPU() string {
	// 1. Check for NVIDIA
	_, err := exec.LookPath("nvidia-smi")
	if err == nil {
		// Verify responsiveness
		err = exec.Command("nvidia-smi", "-L").Run()
		if err == nil {
			log.Println("[System] Auto-detected NVIDIA GPU")
			return "nvidia"
		}
	}

	// 2. Check for Intel (using vainfo or intel_gpu_top)
	_, err = exec.LookPath("vainfo")
	if err == nil {
		out, _ := exec.Command("vainfo").Output()
		if contains(string(out), "Intel") || contains(string(out), "i915") {
			log.Println("[System] Auto-detected Intel GPU via VAAPI")
			return "intel"
		}
	}

	// 3. Check for AMD (using vainfo or amdgpu)
	if err == nil { // if vainfo exists
		out, _ := exec.Command("vainfo").Output()
		if contains(string(out), "AMD") || contains(string(out), "Radeon") {
			log.Println("[System] Auto-detected AMD GPU via VAAPI")
			return "amd"
		}
	}

	log.Println("[System] No hardware acceleration detected, defaulting to CPU")
	return "cpu"
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	// Simple case-insensitive check
	for i := 0; i < len(s)-len(substr)+1; i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] && s[i+j]+32 != substr[j] && s[i+j]-32 != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
