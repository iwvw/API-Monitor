//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"

	"github.com/yusufpapurcu/wmi"
)

var (
	modPdh                          = syscall.NewLazyDLL("pdh.dll")
	procPdhOpenQuery                = modPdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounter        = modPdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData         = modPdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterValue = modPdh.NewProc("PdhGetFormattedCounterValue")
	procPdhCloseQuery               = modPdh.NewProc("PdhCloseQuery")
)

type pdh_fmt_countervalue_double struct {
	CStatus     uint32
	DummyStruct [4]byte // padding for 64-bit alignment
	DoubleValue float64
}

// collectGPUUsagePDH 使用原生 PDH API 获取所有 GPU 的 3D 引擎平均使用率
func (c *Collector) collectGPUUsagePDH() (float64, bool) {
	if runtime.GOOS != "windows" {
		return 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 初始化查询
	if c.pdhQuery == 0 {
		var query uintptr
		ret, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&query)))
		if ret != 0 {
			return 0, false
		}
		c.pdhQuery = query

		// 添加计数器 (使用通配符获取所有 GPU 的 3D 引擎使用率)
		// 使用 English 名称确保兼容性
		counterPath := "\\GPU Engine(*engtype_3D)\\Utilization Percentage"
		pathPtr, _ := syscall.UTF16PtrFromString(counterPath)
		var counter uintptr
		ret, _, _ = procPdhAddEnglishCounter.Call(c.pdhQuery, uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(unsafe.Pointer(&counter)))
		if ret != 0 {
			procPdhCloseQuery.Call(c.pdhQuery)
			c.pdhQuery = 0
			return 0, false
		}
		c.pdhCounter = counter

		// 第一次采集建立基准
		procPdhCollectQueryData.Call(c.pdhQuery)
		return 0, true
	}

	// 执行采集
	ret, _, _ := procPdhCollectQueryData.Call(c.pdhQuery)
	if ret != 0 {
		return 0, false
	}

	// 获取格式化后的值
	var value pdh_fmt_countervalue_double
	const PDH_FMT_DOUBLE = 0x00000200
	ret, _, _ = procPdhGetFormattedCounterValue.Call(c.pdhCounter, PDH_FMT_DOUBLE, 0, uintptr(unsafe.Pointer(&value)))
	if ret != 0 {
		return 0, false
	}

	return value.DoubleValue, true
}

// NVIDIA NVML 原生支持 (Windows 版)
func (c *Collector) collectNvidiaGPUStateNative() (float64, uint64, float64, float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.nvmlInitialized {
		if c.nvmlLib == nil {
			c.nvmlLib = syscall.NewLazyDLL("nvml.dll")
		}

		lib := c.nvmlLib.(*syscall.LazyDLL)
		// 尝试初始化
		initProc := lib.NewProc("nvmlInit_v2")
		if err := initProc.Find(); err != nil {
			return 0, 0, 0, 0, false
		}
		ret, _, _ := initProc.Call()
		if ret != 0 {
			return 0, 0, 0, 0, false
		}
		c.nvmlInitialized = true
	}

	lib := c.nvmlLib.(*syscall.LazyDLL)
	// 获取第一个设备的句柄 (简化处理)
	getHandle := lib.NewProc("nvmlDeviceGetHandleByIndex_v2")
	var device uintptr
	ret, _, _ := getHandle.Call(0, uintptr(unsafe.Pointer(&device)))
	if ret != 0 {
		return 0, 0, 0, 0, false
	}

	// 获取利用率
	getUtil := lib.NewProc("nvmlDeviceGetUtilizationRates")
	var util struct {
		GPU    uint32
		Memory uint32
	}
	ret, _, _ = getUtil.Call(device, uintptr(unsafe.Pointer(&util)))
	if ret != 0 {
		return 0, 0, 0, 0, false
	}

	// 获取显存
	getMem := lib.NewProc("nvmlDeviceGetMemoryInfo")
	var mem struct {
		Total uint64
		Free  uint64
		Used  uint64
	}
	ret, _, _ = getMem.Call(device, uintptr(unsafe.Pointer(&mem)))

	// 获取功耗 (单位通常是毫瓦)
	getPower := lib.NewProc("nvmlDeviceGetPowerUsage")
	var power uint32
	ret, _, _ = getPower.Call(device, uintptr(unsafe.Pointer(&power)))

	// 获取温度 (单位通常是摄氏度)
	var gpuTemp float64 = 0
	getTemp := lib.NewProc("nvmlDeviceGetTemperature")
	if getTemp.Find() == nil {
		var tempVal uint32
		// sensorType = 0 (NVML_TEMPERATURE_GPU)
		ret, _, _ = getTemp.Call(device, 0, uintptr(unsafe.Pointer(&tempVal)))
		if ret == 0 {
			gpuTemp = float64(tempVal)
		}
	}

	return float64(util.GPU), mem.Used, float64(power) / 1000.0, gpuTemp, true
}

type Win32_PerfFormattedData_Counters_ThermalZoneInformation struct {
	HighPrecisionTemperature uint32
}

type MSAcpi_ThermalZoneTemperature struct {
	CurrentTemperature uint32
}

// getCPUTempWindows 获取 Windows 系统下的 CPU 温度
func getCPUTempWindows() float64 {
	// 1. 尝试查询 Win32_PerfFormattedData_Counters_ThermalZoneInformation (普通用户权限即可)
	var tz []Win32_PerfFormattedData_Counters_ThermalZoneInformation
	q := wmi.CreateQuery(&tz, "")
	err := wmi.Query(q, &tz)
	if err == nil && len(tz) > 0 {
		var maxTemp float64 = 0
		for _, t := range tz {
			if t.HighPrecisionTemperature > 0 {
				// HighPrecisionTemperature 单位为分开尔文 (decikelvin, 1/10 K)
				// 转换公式: C = (dK / 10) - 273.15
				c := (float64(t.HighPrecisionTemperature) / 10.0) - 273.15
				if c > maxTemp && c < 150 { // 过滤异常大值
					maxTemp = c
				}
			}
		}
		if maxTemp > 0 {
			return maxTemp
		}
	}

	// 2. 回退到 MSAcpi_ThermalZoneTemperature (需要管理员权限)
	var tzAcpi []MSAcpi_ThermalZoneTemperature
	qAcpi := "SELECT CurrentTemperature FROM MSAcpi_ThermalZoneTemperature"
	err = wmi.QueryNamespace(qAcpi, &tzAcpi, "root\\wmi")
	if err == nil && len(tzAcpi) > 0 {
		var maxTemp float64 = 0
		for _, t := range tzAcpi {
			if t.CurrentTemperature > 0 {
				// CurrentTemperature 单位为分开尔文 (decikelvin, 10ths of a Kelvin)
				c := (float64(t.CurrentTemperature) / 10.0) - 273.15
				if c > maxTemp && c < 150 {
					maxTemp = c
				}
			}
		}
		if maxTemp > 0 {
			return maxTemp
		}
	}

	return 0
}
