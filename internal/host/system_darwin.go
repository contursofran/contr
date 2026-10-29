//go:build darwin

package host

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// Mach constants from <mach/host_info.h>.
const (
	hostCPULoadInfo   = 3  // HOST_CPU_LOAD_INFO
	hostVMInfo64      = 4  // HOST_VM_INFO64
	cpuLoadInfoCount  = 4  // CPU_STATE_MAX words: user, system, idle, nice
	vmInfo64Count     = 38 // words in vm_statistics64
	cpuUser, cpuSys   = 0, 1
	cpuIdle, cpuNice  = 2, 3
	vmFree, vmInact   = 0, 2
	vmPurgeable       = 22 // after four 32-bit and nine 64-bit counters
	machOK            = 0
	libSystemPath     = "/usr/lib/libSystem.B.dylib"
	machHostSelfName  = "mach_host_self"
	hostStatisticsFn  = "host_statistics"
	hostStatistics64N = "host_statistics64"
)

// systemReader reads CPU and memory through Mach's host calls, which libSystem
// exports. purego loads them without cgo.
type systemReader struct {
	host       uint32 // the host port, taken once; each mach_host_self call adds a send right
	hostStats  func(host uint32, flavor int32, info *uint32, count *uint32) int32
	hostStats6 func(host uint32, flavor int32, info *uint32, count *uint32) int32

	mu       sync.Mutex
	prevBusy uint64
	prevAll  uint64
	hasPrev  bool
}

// SystemReader returns a reader of this machine. A failure to load the
// system calls shows on each reading as an error.
func SystemReader() Reader {
	r := &systemReader{}
	lib, err := purego.Dlopen(libSystemPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return func() (Stats, error) { return Stats{}, fmt.Errorf("load libSystem: %w", err) }
	}
	var hostSelf func() uint32
	purego.RegisterLibFunc(&hostSelf, lib, machHostSelfName)
	r.host = hostSelf()
	purego.RegisterLibFunc(&r.hostStats, lib, hostStatisticsFn)
	purego.RegisterLibFunc(&r.hostStats6, lib, hostStatistics64N)
	return r.read
}

func (r *systemReader) read() (Stats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return Stats{}, fmt.Errorf("hw.memsize: %w", err)
	}
	pageSize, err := unix.SysctlUint32("vm.pagesize")
	if err != nil {
		return Stats{}, fmt.Errorf("vm.pagesize: %w", err)
	}
	var vm [vmInfo64Count]uint32
	n := uint32(vmInfo64Count)
	if kr := r.hostStats6(r.host, hostVMInfo64, &vm[0], &n); kr != machOK {
		return Stats{}, fmt.Errorf("host_statistics64: kern_return %d", kr)
	}
	// Available memory is what the system can give back at once: free
	// (includes speculative), inactive and purgeable pages.
	available := (uint64(vm[vmFree]) + uint64(vm[vmInact]) + uint64(vm[vmPurgeable])) * uint64(pageSize)
	used := total - min(available, total)

	var cpu [cpuLoadInfoCount]uint32
	c := uint32(cpuLoadInfoCount)
	if kr := r.hostStats(r.host, hostCPULoadInfo, &cpu[0], &c); kr != machOK {
		return Stats{}, fmt.Errorf("host_statistics: kern_return %d", kr)
	}
	return Stats{CPUPercent: r.cpuPercent(cpu), MemUsed: used, MemTotal: total}, nil
}

// cpuPercent is the busy share of the ticks since the previous reading, or
// since boot for the first one.
func (r *systemReader) cpuPercent(t [cpuLoadInfoCount]uint32) float32 {
	busy := uint64(t[cpuUser]) + uint64(t[cpuSys]) + uint64(t[cpuNice])
	all := busy + uint64(t[cpuIdle])
	var db, da uint64 = busy, all
	if r.hasPrev {
		db, da = busy-r.prevBusy, all-r.prevAll
	}
	r.prevBusy, r.prevAll, r.hasPrev = busy, all, true
	if da == 0 {
		return 0
	}
	return float32(float64(db) / float64(da) * 100)
}
