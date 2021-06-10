package cpu

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

const cgroupRootDir = "/sys/fs/cgroup"

// cgroup Linux cgroup
type cgroup struct {
	cgroupSet map[string]string
}

// CPUCFSQuotaUs cpu.cfs_quota_us
// 此参数可以设定在某一阶段（由 cpu.cfs_period_us 规定）某个 cgroup 中所有任务可运行的时间总量，单位为微秒（µs，这里以 "us" 代表）。一旦 cgroup 中任务用完按配额分得的时间，它们就会被在此阶段的时间提醒限制流量，并在进入下阶段前禁止运行。
// -1，这表示 cgroup 不需要遵循任何 CPU 时间限制。
/**
cat /sys/fs/cgroup/cpu/cpu.cfs_quota_us
-1
 */
func (c *cgroup) CPUCFSQuotaUs() (int64, error) {
	data, err := readFile(path.Join(c.cgroupSet["cpu"], "cpu.cfs_quota_us"))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(data, 10, 64)
}

// CPUCFSPeriodUs cpu.cfs_period_us
// 此参数可以设定重新分配 cgroup 可用 CPU 资源的时间间隔，单位为微秒（µs，这里以 “us” 表示）。
/**
cat /sys/fs/cgroup/cpu/cpu.cfs_period_us
100000
 */
func (c *cgroup) CPUCFSPeriodUs() (uint64, error) {
	data, err := readFile(path.Join(c.cgroupSet["cpu"], "cpu.cfs_period_us"))
	if err != nil {
		return 0, err
	}
	return parseUint(data)
}

// CPUAcctUsage cpuacct.usage
// 所有cpu总的使用时间 单位ns
/**
cat /sys/fs/cgroup/cpuacct/cpuacct.usage
71334690177595048
 */
func (c *cgroup) CPUAcctUsage() (uint64, error) {
	data, err := readFile(path.Join(c.cgroupSet["cpuacct"], "cpuacct.usage"))
	if err != nil {
		return 0, err
	}
	return parseUint(data)
}

// CPUAcctUsagePerCPU cpuacct.usage_percpu
// 各个cpu使用的时间 单位ns
/**
cat /sys/fs/cgroup/cpuacct/cpuacct.usage_percpu
1687529415850196 992137354315412 824879761303644
 */
func (c *cgroup) CPUAcctUsagePerCPU() ([]uint64, error) {
	data, err := readFile(path.Join(c.cgroupSet["cpuacct"], "cpuacct.usage_percpu"))
	if err != nil {
		return nil, err
	}
	var usage []uint64
	for _, v := range strings.Fields(string(data)) {
		var u uint64
		if u, err = parseUint(v); err != nil {
			return nil, err
		}
		// fix possible_cpu:https://www.ibm.com/support/knowledgecenter/en/linuxonibm/com.ibm.linux.z.lgdd/lgdd_r_posscpusparm.html
		if u != 0 {
			usage = append(usage, u)
		}
	}
	return usage, nil
}

// CPUSetCPUs cpuset.cpus
// cpuset.cpus: 可以使用的cpu节点 cpu绑定
func (c *cgroup) CPUSetCPUs() ([]uint64, error) {
	data, err := readFile(path.Join(c.cgroupSet["cpuset"], "cpuset.cpus"))
	if err != nil {
		return nil, err
	}
	cpus, err := ParseUintList(data)
	if err != nil {
		return nil, err
	}
	var sets []uint64
	for k := range cpus {
		sets = append(sets, uint64(k))
	}
	return sets, nil
}

// CurrentcGroup get current process cgroup
/**
解析pid中各个子系统在/sys/fs/cgroup中的具体目录
/proc/cgroups中的hierarchy:子系统:进程在 cgroup 树中的路径，即进程所属的 cgroup。/sys/fs/cgroup/中的目录
cat /proc/39737/cgroup
	11:blkio:/user.slice
	10:cpuset:/    					设置CPU的亲和性，可以限制cgroup中的进程只能在指定的CPU上运行，或者不能在指定的CPU上运行，同时cpuset还能设置内存的亲和性。
	9:freezer:/
	8:cpuacct,cpu:/user.slice		cpuacct:包含当前cgroup所使用的CPU的统计信息 cpu:限制 CPU 时间片的分配
	7:memory:/user.slice
	6:pids:/user.slice
	5:devices:/user.slice
	4:perf_event:/
	3:net_prio,net_cls:/
	2:hugetlb:/
	1:name=systemd:/user.slice/user-1023.slice/session-426822.scope
 */
func currentcGroup() (*cgroup, error) {
	pid := os.Getpid()
	cgroupFile := fmt.Sprintf("/proc/%d/cgroup", pid)
	cgroupSet := make(map[string]string)
	fp, err := os.Open(cgroupFile)
	if err != nil {
		return nil, err
	}
	defer fp.Close()
	buf := bufio.NewReader(fp)
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		col := strings.Split(strings.TrimSpace(line), ":")
		if len(col) != 3 {
			return nil, fmt.Errorf("invalid cgroup format %s", line)
		}
		dir := col[2]
		// When dir is not equal to /, it must be in docker
		if dir != "/" {
			cgroupSet[col[1]] = path.Join(cgroupRootDir, col[1])
			if strings.Contains(col[1], ",") {
				for _, k := range strings.Split(col[1], ",") {
					cgroupSet[k] = path.Join(cgroupRootDir, k)
				}
			}
		} else {
			cgroupSet[col[1]] = path.Join(cgroupRootDir, col[1], col[2])
			if strings.Contains(col[1], ",") {
				for _, k := range strings.Split(col[1], ",") {
					cgroupSet[k] = path.Join(cgroupRootDir, k, col[2])
				}
			}
		}
	}
	return &cgroup{cgroupSet: cgroupSet}, nil
}
