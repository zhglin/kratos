package cpu

import (
	"bufio"
	"fmt"
	"github.com/pkg/errors"
	pscpu "github.com/shirou/gopsutil/cpu"
	"os"
	"strconv"
	"strings"
)

type cgroupCPU struct {
	frequency uint64	// cpu主频
	quota     float64	// 当前进程能使用的cpu核数 没做限制就是所有cpu核数
	cores     uint64	// 总的cpu逻辑核数

	preSystem uint64	// 一个cpu当前总的使用时长 单位ns    	上一次
	preTotal  uint64	// 当前进程使用的cpu总的时长 单位ns		上一次
	usage     uint64
}

func newCgroupCPU() (cpu *cgroupCPU, err error) {
	var cores int
	cores, err = pscpu.Counts(true) // 总的cpu逻辑核数
	if err != nil || cores == 0 {
		var cpus []uint64
		cpus, err = perCPUUsage()
		if err != nil {
			err = errors.Errorf("perCPUUsage() failed!err:=%v", err)
			return
		}
		cores = len(cpus)
	}

	sets, err := cpuSets()	// 当前pid绑定的cpu数
	if err != nil {
		err = errors.Errorf("cpuSets() failed!err:=%v", err)
		return
	}

	/**
	* cfs_period_us,cfs_quota_us两个文件配合起来设置CPU的使用上限。
	* 1.限制只能使用1个CPU（每250ms能使用250ms的CPU时间）
	*   echo 250000 > cpu.cfs_quota_us  quota = 250ms
	* 	echo 250000 > cpu.cfs_period_us  period = 250ms
	*
	* 2.限制使用2个CPU（内核）（每500ms能使用1000ms的CPU时间，即使用两个内核）
	* 	echo 1000000 > cpu.cfs_quota_us  quota = 1000ms
	* 	echo 500000 > cpu.cfs_period_us  period = 500ms
	*
	* 3.限制使用1个CPU的20%（每50ms能使用10ms的CPU时间，即使用一个CPU核心的20%）
	* 	echo 10000 > cpu.cfs_quota_us  quota = 10ms
	* 	echo 50000 > cpu.cfs_period_us  period = 50ms
	 */
	quota := float64(len(sets))	// 当前进程能使用的cpu核数 没做限制就是所有cpu核数
	cq, err := cpuQuota()
	if err == nil && cq != -1 {
		var period uint64
		if period, err = cpuPeriod(); err != nil {
			err = errors.Errorf("cpuPeriod() failed!err:=%v", err)
			return
		}
		limit := float64(cq) / float64(period)
		if limit < quota {
			quota = limit
		}
	}
	maxFreq := cpuMaxFreq() // cpu主频

	preSystem, err := systemCPUUsage() // 一个cpu当前总的使用时长 单位ns
	if err != nil {
		err = errors.Errorf("systemCPUUsage() failed!err:=%v", err)
		return
	}
	preTotal, err := totalCPUUsage() // 当前进程使用的cpu总的时长 单位ns
	if err != nil {
		err = errors.Errorf("totalCPUUsage() failed!err:=%v", err)
		return
	}
	cpu = &cgroupCPU{
		frequency: maxFreq,
		quota:     quota,
		cores:     uint64(cores),
		preSystem: preSystem,
		preTotal:  preTotal,
	}
	return
}

func (cpu *cgroupCPU) Usage() (u uint64, err error) {
	var (
		total  uint64
		system uint64
	)
	total, err = totalCPUUsage() // 当前时刻 进程使用的cpu总时间
	if err != nil {
		return
	}
	system, err = systemCPUUsage()	// 当前时刻 系统的一个cpu使用总时间
	if err != nil {
		return
	}
	if system != cpu.preSystem {
		// (float64(system-cpu.preSystem) * cpu.quota)  system-cpu.preSystem(当前进程所能使用的一个cpu的所有时间) * cpu.quota (当前进程绑定的cpu数)  结果就等于当前进程这段时间所能使用的所有cpu时间
		u = uint64(float64((total-cpu.preTotal)*cpu.cores*1e3) / (float64(system-cpu.preSystem) * cpu.quota))
	}
	cpu.preSystem = system
	cpu.preTotal = total
	return
}

func (cpu *cgroupCPU) Info() Info {
	return Info{
		Frequency: cpu.frequency,
		Quota:     cpu.quota,
	}
}

const nanoSecondsPerSecond = 1e9

// ErrNoCFSLimit is no quota limit
var ErrNoCFSLimit = errors.Errorf("no quota limit")

var clockTicksPerSecond = uint64(getClockTicks())

// systemCPUUsage returns the host system's cpu usage in
// nanoseconds. An error is returned if the format of the underlying
// file does not match.
//
// Uses /proc/stat defined by POSIX. Looks for the cpu
// statistics line and then sums up the first seven fields
// provided. See man 5 proc for details on specific field
// information.
// 一个cpu耗时的总和 单位ns
func systemCPUUsage() (usage uint64, err error) {
	var (
		line string
		f    *os.File
	)
	if f, err = os.Open("/proc/stat"); err != nil {
		return
	}
	bufReader := bufio.NewReaderSize(nil, 128)
	defer func() {
		bufReader.Reset(nil)
		f.Close()
	}()
	bufReader.Reset(f)
	for err == nil {
		if line, err = bufReader.ReadString('\n'); err != nil {
			err = errors.WithStack(err)
			return
		}
		parts := strings.Fields(line)
		switch parts[0] {
		case "cpu":
			if len(parts) < 8 {
				err = errors.WithStack(fmt.Errorf("bad format of cpu stats"))
				return
			}
			var totalClockTicks uint64
			for _, i := range parts[1:8] { // 处在不同状态下的运行时间总和，时间单位sysconf(_SC_CLK_TCK)一般地定义为jiffies(一般地等于10ms)
				var v uint64
				if v, err = strconv.ParseUint(i, 10, 64); err != nil {
					err = errors.WithStack(fmt.Errorf("error parsing cpu stats"))
					return
				}
				totalClockTicks += v
			}
			usage = (totalClockTicks * nanoSecondsPerSecond) / clockTicksPerSecond // totalClockTicks(被当做了秒) * nanoSecondsPerSecond (总的纳秒数)
			return
		}
	}
	err = errors.Errorf("bad stats format")
	return
}

func totalCPUUsage() (usage uint64, err error) {
	var cg *cgroup
	if cg, err = currentcGroup(); err != nil {
		return
	}
	return cg.CPUAcctUsage()
}

func perCPUUsage() (usage []uint64, err error) {
	var cg *cgroup
	if cg, err = currentcGroup(); err != nil {
		return
	}
	return cg.CPUAcctUsagePerCPU()
}

func cpuSets() (sets []uint64, err error) {
	var cg *cgroup
	if cg, err = currentcGroup(); err != nil {
		return
	}
	return cg.CPUSetCPUs()
}

func cpuQuota() (quota int64, err error) {
	var cg *cgroup
	if cg, err = currentcGroup(); err != nil {
		return
	}
	return cg.CPUCFSQuotaUs()
}

func cpuPeriod() (peroid uint64, err error) {
	var cg *cgroup
	if cg, err = currentcGroup(); err != nil {
		return
	}
	return cg.CPUCFSPeriodUs()
}

// cpu主频
func cpuFreq() uint64 {
	lines, err := readLines("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	for _, line := range lines {
		fields := strings.Split(line, ":")
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSpace(fields[0])
		value := strings.TrimSpace(fields[1])
		if key == "cpu MHz" || key == "clock" {
			// treat this as the fallback value, thus we ignore error
			if t, err := strconv.ParseFloat(strings.Replace(value, "MHz", "", 1), 64); err == nil {
				return uint64(t * 1000.0 * 1000.0)
			}
		}
	}
	return 0
}

// cpu最大主频 linux支持动态主频调整
func cpuMaxFreq() uint64 {
	feq := cpuFreq()  // /proc/cpuinfo里的主频
	data, err := readFile("/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq")
	if err != nil {
		return feq
	}
	// override the max freq from /proc/cpuinfo
	cfeq, err := parseUint(data)
	if err == nil {
		feq = cfeq
	}
	return feq
}

//GetClockTicks get the OS's ticks per second
// 检索性能计数器的频率，操作系统的性能统计分辨率，也就是每秒钟统计多少次的意思。
// https://lrita.github.io/images/posts/linux/Linux_CPU_Usage_Analysis.pdf
func getClockTicks() int {
	// TODO figure out a better alternative for platforms where we're missing cgo
	//
	// TODO Windows. This could be implemented using Win32 QueryPerformanceFrequency().
	// https://msdn.microsoft.com/en-us/library/windows/desktop/ms644905(v=vs.85).aspx
	//
	// An example of its usage can be found here.
	// https://msdn.microsoft.com/en-us/library/windows/desktop/dn553408(v=vs.85).aspx

	return 100
}
