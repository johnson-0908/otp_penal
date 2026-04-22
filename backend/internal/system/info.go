package system

import (
	"context"
	"runtime"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type Overview struct {
	SampledAt time.Time    `json:"sampled_at"`
	Host      HostInfo     `json:"host"`
	CPU       CPUInfo      `json:"cpu"`
	Memory    MemInfo      `json:"memory"`
	Swap      MemInfo      `json:"swap"`
	Disks     []DiskInfo   `json:"disks"`
	DiskIO    DiskIOInfo   `json:"disk_io"`
	Network   NetInfo      `json:"network"`
	NetIfaces []NetIfInfo  `json:"network_interfaces"`
	Load      *LoadInfo    `json:"load,omitempty"`
	Procs     ProcInfo     `json:"processes"`
	Runtime   Runtime      `json:"runtime"`
}

type HostInfo struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Platform     string `json:"platform"`
	Version      string `json:"version"`
	KernelArch   string `json:"kernel_arch"`
	UptimeSec    uint64 `json:"uptime_sec"`
	BootTimeUnix uint64 `json:"boot_time_unix"`
}

type CPUInfo struct {
	Model        string    `json:"model"`
	PhysicalCore int       `json:"physical_cores"`
	LogicalCore  int       `json:"logical_cores"`
	PercentTotal float64   `json:"percent_total"`
	PerCore      []float64 `json:"per_core"`
}

type MemInfo struct {
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	FreeBytes  uint64  `json:"free_bytes"`
	Percent    float64 `json:"percent"`
}

type DiskInfo struct {
	Mount      string  `json:"mount"`
	FSType     string  `json:"fstype"`
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	Percent    float64 `json:"percent"`
}

type DiskIOInfo struct {
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
	ReadCount  uint64 `json:"read_count"`
	WriteCount uint64 `json:"write_count"`
}

type NetInfo struct {
	BytesSent   uint64 `json:"bytes_sent"`
	BytesRecv   uint64 `json:"bytes_recv"`
	PacketsSent uint64 `json:"packets_sent"`
	PacketsRecv uint64 `json:"packets_recv"`
	Connections int    `json:"connections"`
}

type NetIfInfo struct {
	Name      string `json:"name"`
	BytesSent uint64 `json:"bytes_sent"`
	BytesRecv uint64 `json:"bytes_recv"`
}

type LoadInfo struct {
	One     float64 `json:"1m"`
	Five    float64 `json:"5m"`
	Fifteen float64 `json:"15m"`
}

type ProcInfo struct {
	Total   int `json:"total"`
	Running int `json:"running"`
}

type Runtime struct {
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	NumCPU    int    `json:"num_cpu"`
}

func GetOverview(ctx context.Context) (*Overview, error) {
	o := &Overview{
		SampledAt: time.Now().UTC(),
		Runtime: Runtime{
			GoVersion: runtime.Version(),
			GOOS:      runtime.GOOS,
			GOARCH:    runtime.GOARCH,
			NumCPU:    runtime.NumCPU(),
		},
	}

	if hi, err := host.InfoWithContext(ctx); err == nil {
		o.Host = HostInfo{
			Hostname: hi.Hostname, OS: hi.OS, Platform: hi.Platform,
			Version: hi.PlatformVersion, KernelArch: hi.KernelArch,
			UptimeSec: hi.Uptime, BootTimeUnix: hi.BootTime,
		}
		if hi.Procs > 0 {
			o.Procs.Running = int(hi.Procs)
		}
	}

	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		o.CPU.Model = infos[0].ModelName
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		o.CPU.PhysicalCore = n
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		o.CPU.LogicalCore = n
	}
	// One call with interval=250ms: gopsutil takes two samples internally,
	// so we always get valid deltas (no "first-call returns 100%" artifact).
	// Compute total from per-core — more consistent than a second call.
	if p, err := cpu.PercentWithContext(ctx, 250*time.Millisecond, true); err == nil && len(p) > 0 {
		o.CPU.PerCore = p
		var sum float64
		for _, v := range p {
			sum += v
		}
		o.CPU.PercentTotal = sum / float64(len(p))
	}

	if m, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		o.Memory = MemInfo{m.Total, m.Used, m.Free, m.UsedPercent}
	}
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		o.Swap = MemInfo{sw.Total, sw.Used, sw.Free, sw.UsedPercent}
	}

	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		for _, p := range parts {
			u, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil {
				continue
			}
			o.Disks = append(o.Disks, DiskInfo{
				Mount: p.Mountpoint, FSType: p.Fstype,
				TotalBytes: u.Total, UsedBytes: u.Used, Percent: u.UsedPercent,
			})
		}
	}

	if io, err := disk.IOCountersWithContext(ctx); err == nil {
		for _, v := range io {
			o.DiskIO.ReadBytes += v.ReadBytes
			o.DiskIO.WriteBytes += v.WriteBytes
			o.DiskIO.ReadCount += v.ReadCount
			o.DiskIO.WriteCount += v.WriteCount
		}
	}

	// Build the aggregate Network counters ONLY from "real" NICs
	// (has MAC + up + not loopback). Windows enumerates a lot of virtual
	// adapters (Hyper-V, WSL, VPN tunnels, Teredo...) whose cumulative
	// counters are often garbage and blow up the delta-based rate.
	if ifs, err := net.IOCountersWithContext(ctx, true); err == nil {
		metaList, _ := net.InterfacesWithContext(ctx)
		meta := make(map[string]net.InterfaceStat, len(metaList))
		for _, m := range metaList {
			meta[m.Name] = m
		}
		isRealNIC := func(name string) bool {
			m, ok := meta[name]
			if !ok || m.HardwareAddr == "" {
				return false
			}
			up := false
			for _, f := range m.Flags {
				if f == "loopback" {
					return false
				}
				if f == "up" {
					up = true
				}
			}
			return up
		}
		for _, v := range ifs {
			if !isRealNIC(v.Name) {
				continue
			}
			if v.BytesSent == 0 && v.BytesRecv == 0 {
				continue
			}
			o.NetIfaces = append(o.NetIfaces, NetIfInfo{
				Name: v.Name, BytesSent: v.BytesSent, BytesRecv: v.BytesRecv,
			})
			o.Network.BytesSent += v.BytesSent
			o.Network.BytesRecv += v.BytesRecv
			o.Network.PacketsSent += v.PacketsSent
			o.Network.PacketsRecv += v.PacketsRecv
		}
		sort.Slice(o.NetIfaces, func(i, j int) bool {
			return (o.NetIfaces[i].BytesSent + o.NetIfaces[i].BytesRecv) >
				(o.NetIfaces[j].BytesSent + o.NetIfaces[j].BytesRecv)
		})
		if len(o.NetIfaces) > 6 {
			o.NetIfaces = o.NetIfaces[:6]
		}
	}
	if conns, err := net.ConnectionsWithContext(ctx, "all"); err == nil {
		o.Network.Connections = len(conns)
	}

	if runtime.GOOS != "windows" {
		if la, err := load.AvgWithContext(ctx); err == nil {
			o.Load = &LoadInfo{la.Load1, la.Load5, la.Load15}
		}
	}

	if pids, err := process.PidsWithContext(ctx); err == nil {
		o.Procs.Total = len(pids)
	}

	return o, nil
}
