package cpu

import (
	"time"

	"github.com/shirou/gopsutil/cpu"
)

type psutilCPU struct {
	interval time.Duration
}

func newPsutilCPU(interval time.Duration) (cpu *psutilCPU, err error) {
	cpu = &psutilCPU{interval: interval}
	_, err = cpu.Usage()
	if err != nil {
		return
	}
	return
}

// Usage Percent计算每个cpu或合并cpu使用的百分比。如果给定的间隔为0，它将把当前cpu时间与上次调用进行比较。每个cpu返回一个值，如果percpu设置为false则返回单个值。
func (ps *psutilCPU) Usage() (u uint64, err error) {
	var percents []float64
	percents, err = cpu.Percent(ps.interval, false) // 这里会sleep(interval)
	if err == nil {
		u = uint64(percents[0] * 10)
	}
	return
}

func (ps *psutilCPU) Info() (info Info) {
	stats, err := cpu.Info()	//cpu 信息
	if err != nil {
		return
	}
	cores, err := cpu.Counts(true)	// 逻辑cpu核心数
	if err != nil {
		return
	}
	info = Info{
		Frequency: uint64(stats[0].Mhz),
		Quota:     float64(cores),
	}
	return
}
