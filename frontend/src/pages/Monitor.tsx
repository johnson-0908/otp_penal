import { useQuery } from "@tanstack/react-query";
import { Activity, Cpu, HardDrive, MemoryStick, Network, Server, Timer, Wifi } from "lucide-react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "../api";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Separator } from "../components/ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { Badge } from "../components/ui/badge";
import { MetricCard } from "../components/MetricCard";
import { useMetricsHistory } from "../hooks/useMetricsHistory";
import { cn, formatBytes, formatDuration, formatRate } from "../lib/utils";

type Overview = {
  sampled_at: string;
  host: {
    hostname: string;
    os: string;
    platform: string;
    version: string;
    kernel_arch: string;
    uptime_sec: number;
  };
  cpu: {
    model: string;
    physical_cores: number;
    logical_cores: number;
    percent_total: number;
    per_core: number[];
  };
  memory: { total_bytes: number; used_bytes: number; percent: number };
  swap: { total_bytes: number; used_bytes: number; percent: number };
  disks: Array<{ mount: string; fstype: string; total_bytes: number; used_bytes: number; percent: number }>;
  disk_io: { read_bytes: number; write_bytes: number; read_count: number; write_count: number };
  network: { bytes_sent: number; bytes_recv: number; connections: number };
  network_interfaces: Array<{ name: string; bytes_sent: number; bytes_recv: number }>;
  load?: { "1m": number; "5m": number; "15m": number };
  processes: { total: number; running: number };
  runtime: { go_version: string; goos: string; goarch: string };
};

function coreColor(p: number) {
  if (p >= 85) return "bg-red-500";
  if (p >= 60) return "bg-amber-500";
  return "bg-emerald-500";
}

function timeLabel(t: number) {
  const d = new Date(t);
  return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}:${d.getSeconds().toString().padStart(2, "0")}`;
}

export default function Monitor() {
  const { data, isFetching, error } = useQuery<Overview>({
    queryKey: ["overview"],
    queryFn: () => api<Overview>("/api/system/overview"),
    refetchInterval: 2000,
  });
  const history = useMetricsHistory(data);

  const chartData = history.map((s) => ({
    t: s.t,
    tLabel: timeLabel(s.t),
    cpu: Number(s.cpu.toFixed(1)),
    mem: Number(s.mem.toFixed(1)),
    netTx: s.netTxRate < 0 ? null : s.netTxRate,
    netRx: s.netRxRate < 0 ? null : s.netRxRate,
    diskR: s.diskReadRate < 0 ? null : s.diskReadRate,
    diskW: s.diskWriteRate < 0 ? null : s.diskWriteRate,
  }));

  const latest = history[history.length - 1];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-end justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">性能监控</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {data ? (
              <>
                <span className="font-mono">{data.host.hostname}</span>
                <span className="mx-2">·</span>
                {data.host.platform} {data.host.version}
                <span className="mx-2">·</span>
                uptime {formatDuration(data.host.uptime_sec)}
              </>
            ) : (
              "加载中..."
            )}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {isFetching && (
            <Badge variant="outline" className="gap-1.5">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-500" />
              实时
            </Badge>
          )}
          {data?.load && (
            <Badge variant="secondary" className="font-mono">
              load {data.load["1m"].toFixed(2)} / {data.load["5m"].toFixed(2)} / {data.load["15m"].toFixed(2)}
            </Badge>
          )}
        </div>
      </div>

      {error && (
        <Card>
          <CardContent className="pt-6 text-sm text-destructive">加载失败：{String(error)}</CardContent>
        </Card>
      )}

      {/* Summary cards */}
      <section className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
        <MetricCard
          label="CPU"
          icon={<Cpu className="h-3.5 w-3.5" />}
          value={`${data?.cpu.percent_total.toFixed(1) ?? "—"}%`}
          sub={data ? `${data.cpu.physical_cores} 核 / ${data.cpu.logical_cores} 线程` : undefined}
          percent={data?.cpu.percent_total}
          series={history.map((s) => ({ t: s.t, v: s.cpu }))}
          yMax={100}
        />
        <MetricCard
          label="内存"
          icon={<MemoryStick className="h-3.5 w-3.5" />}
          value={`${data?.memory.percent.toFixed(1) ?? "—"}%`}
          sub={data ? `${formatBytes(data.memory.used_bytes)} / ${formatBytes(data.memory.total_bytes)}` : undefined}
          percent={data?.memory.percent}
          series={history.map((s) => ({ t: s.t, v: s.mem }))}
          yMax={100}
        />
        <MetricCard
          label="网络 ↓"
          icon={<Network className="h-3.5 w-3.5" />}
          value={latest && latest.netRxRate >= 0 ? formatRate(latest.netRxRate) : "—"}
          sub={data ? `共接收 ${formatBytes(data.network.bytes_recv)}` : undefined}
          tone="info"
          series={history.filter((s) => s.netRxRate >= 0).map((s) => ({ t: s.t, v: s.netRxRate }))}
        />
        <MetricCard
          label="运行时长"
          icon={<Timer className="h-3.5 w-3.5" />}
          value={data ? formatDuration(data.host.uptime_sec) : "—"}
          sub={data ? `${data.processes.total} 进程 · ${data.network.connections} 连接` : undefined}
          tone="info"
        />
      </section>

      {/* CPU chart */}
      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <div>
            <CardTitle className="text-base">CPU 使用率</CardTitle>
            <p className="text-xs text-muted-foreground">最近 {history.length} 个采样 · 每 2 秒</p>
          </div>
          {data && <span className="font-mono text-sm text-muted-foreground">{data.cpu.model}</span>}
        </CardHeader>
        <CardContent>
          <div className="h-48">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData}>
                <defs>
                  <linearGradient id="cpuGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="hsl(var(--chart-1))" stopOpacity={0.3} />
                    <stop offset="100%" stopColor="hsl(var(--chart-1))" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                <XAxis dataKey="tLabel" tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }} stroke="hsl(var(--border))" />
                <YAxis domain={[0, 100]} tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }} stroke="hsl(var(--border))" unit="%" />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: "0.5rem",
                    fontSize: "12px",
                  }}
                  labelStyle={{ color: "hsl(var(--muted-foreground))" }}
                  formatter={(v: number) => [`${v.toFixed(1)}%`, "CPU"]}
                />
                <Area
                  type="monotone"
                  dataKey="cpu"
                  stroke="hsl(var(--chart-1))"
                  strokeWidth={1.5}
                  fill="url(#cpuGrad)"
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>

          {data && data.cpu.per_core.length > 0 && (
            <>
              <Separator className="my-4" />
              <div className="grid grid-cols-4 gap-3 md:grid-cols-8">
                {data.cpu.per_core.map((p, i) => (
                  <div key={i}>
                    <div className="flex items-baseline justify-between text-[10px] text-muted-foreground">
                      <span className="font-mono">CPU{i}</span>
                      <span className="font-mono tabular-nums">{p.toFixed(0)}%</span>
                    </div>
                    <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-secondary">
                      <div className={cn("h-full transition-all", coreColor(p))} style={{ width: `${p}%` }} />
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Memory + Network grid */}
      <section className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">内存</CardTitle>
            <p className="text-xs text-muted-foreground">
              {data
                ? `${formatBytes(data.memory.used_bytes)} / ${formatBytes(data.memory.total_bytes)} · swap ${data.swap.percent.toFixed(0)}%`
                : ""}
            </p>
          </CardHeader>
          <CardContent>
            <div className="h-40">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData}>
                  <defs>
                    <linearGradient id="memGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="hsl(var(--chart-2))" stopOpacity={0.3} />
                      <stop offset="100%" stopColor="hsl(var(--chart-2))" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                  <XAxis dataKey="tLabel" tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }} stroke="hsl(var(--border))" />
                  <YAxis domain={[0, 100]} tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }} stroke="hsl(var(--border))" unit="%" />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: "0.5rem",
                      fontSize: "12px",
                    }}
                    formatter={(v: number) => [`${v.toFixed(1)}%`, "内存"]}
                  />
                  <Area type="monotone" dataKey="mem" stroke="hsl(var(--chart-2))" strokeWidth={1.5} fill="url(#memGrad)" isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">网络吞吐</CardTitle>
            <p className="text-xs text-muted-foreground">
              {latest && latest.netTxRate >= 0
                ? `↑ ${formatRate(latest.netTxRate)}  ↓ ${formatRate(latest.netRxRate)}`
                : "采样中..."}
            </p>
          </CardHeader>
          <CardContent>
            <div className="h-40">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                  <XAxis dataKey="tLabel" tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }} stroke="hsl(var(--border))" />
                  <YAxis
                    tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
                    stroke="hsl(var(--border))"
                    tickFormatter={(v: number) => formatBytes(v)}
                    width={70}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: "0.5rem",
                      fontSize: "12px",
                    }}
                    formatter={(v: number, name) => [formatRate(v), name === "netTx" ? "↑ 上行" : "↓ 下行"]}
                  />
                  <Line type="monotone" dataKey="netTx" stroke="hsl(var(--chart-3))" strokeWidth={1.5} dot={false} isAnimationActive={false} connectNulls />
                  <Line type="monotone" dataKey="netRx" stroke="hsl(var(--chart-1))" strokeWidth={1.5} dot={false} isAnimationActive={false} connectNulls />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>
      </section>

      {/* Disks + disk IO + Interfaces */}
      <section className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <HardDrive className="h-4 w-4" /> 磁盘容量
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {data?.disks.map((d) => (
              <div key={d.mount}>
                <div className="flex items-baseline justify-between text-sm">
                  <span className="font-mono font-medium">{d.mount}</span>
                  <span className="font-mono tabular-nums text-muted-foreground">
                    {formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)} · {d.fstype}
                  </span>
                </div>
                <div className="mt-1.5 h-2 overflow-hidden rounded-full bg-secondary">
                  <div
                    className={cn(
                      "h-full transition-all",
                      d.percent >= 90 ? "bg-red-500" : d.percent >= 75 ? "bg-amber-500" : "bg-emerald-500",
                    )}
                    style={{ width: `${d.percent}%` }}
                  />
                </div>
                <div className="mt-0.5 text-right text-[11px] font-mono text-muted-foreground tabular-nums">
                  {d.percent.toFixed(1)}%
                </div>
              </div>
            ))}
            {(!data || data.disks.length === 0) && <div className="text-sm text-muted-foreground">无数据</div>}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Wifi className="h-4 w-4" /> 网络接口
            </CardTitle>
            <p className="text-xs text-muted-foreground">按累计流量排序前 6</p>
          </CardHeader>
          <CardContent>
            {data?.network_interfaces && data.network_interfaces.length > 0 ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>接口</TableHead>
                    <TableHead className="text-right">↑ 累计上行</TableHead>
                    <TableHead className="text-right">↓ 累计下行</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.network_interfaces.map((i) => (
                    <TableRow key={i.name}>
                      <TableCell className="font-mono">{i.name}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{formatBytes(i.bytes_sent)}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">{formatBytes(i.bytes_recv)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <div className="text-sm text-muted-foreground">无数据</div>
            )}
          </CardContent>
        </Card>
      </section>

      {/* Disk IO + System info */}
      <section className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Activity className="h-4 w-4" /> 磁盘 I/O 速率
            </CardTitle>
            <p className="text-xs text-muted-foreground">
              {latest && latest.diskReadRate >= 0
                ? `读 ${formatRate(latest.diskReadRate)}  写 ${formatRate(latest.diskWriteRate)}`
                : "采样中..."}
            </p>
          </CardHeader>
          <CardContent>
            <div className="h-40">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                  <XAxis dataKey="tLabel" tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }} stroke="hsl(var(--border))" />
                  <YAxis
                    tick={{ fontSize: 10, fill: "hsl(var(--muted-foreground))" }}
                    stroke="hsl(var(--border))"
                    tickFormatter={(v: number) => formatBytes(v)}
                    width={70}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: "0.5rem",
                      fontSize: "12px",
                    }}
                    formatter={(v: number, name) => [formatRate(v), name === "diskR" ? "读" : "写"]}
                  />
                  <Line type="monotone" dataKey="diskR" stroke="hsl(var(--chart-2))" strokeWidth={1.5} dot={false} isAnimationActive={false} connectNulls />
                  <Line type="monotone" dataKey="diskW" stroke="hsl(var(--chart-4))" strokeWidth={1.5} dot={false} isAnimationActive={false} connectNulls />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Server className="h-4 w-4" /> 系统信息
            </CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="space-y-2 text-sm">
              <Row k="Hostname" v={data?.host.hostname ?? "—"} />
              <Row k="OS" v={data ? `${data.host.platform} ${data.host.version}` : "—"} />
              <Row k="Arch" v={data?.host.kernel_arch ?? "—"} />
              <Row k="CPU" v={data?.cpu.model ?? "—"} />
              <Row k="内存" v={data ? formatBytes(data.memory.total_bytes) : "—"} />
              <Row k="Go" v={data ? `${data.runtime.go_version} (${data.runtime.goos}/${data.runtime.goarch})` : "—"} />
              <Row k="进程数" v={data ? `${data.processes.total}` : "—"} />
            </dl>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex gap-4">
      <dt className="w-20 shrink-0 text-muted-foreground">{k}</dt>
      <dd className="flex-1 truncate font-mono text-right">{v}</dd>
    </div>
  );
}
