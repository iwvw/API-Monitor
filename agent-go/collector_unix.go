//go:build !windows

package main

// collectGPUUsagePDH Windows-only stub
func (c *Collector) collectGPUUsagePDH() (float64, bool) {
	return 0, false
}

// collectNvidiaGPUStateNative Non-Windows stub
// (On Linux it currently falls back to nvidia-smi command line)
func (c *Collector) collectNvidiaGPUStateNative() (float64, uint64, float64, float64, bool) {
	return 0, 0, 0, 0, false
}

// getCPUTempWindows is a stub for non-Windows platforms
func getCPUTempWindows() float64 {
	return 0
}

