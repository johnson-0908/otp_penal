import { ReactNode } from "react";
import { Area, AreaChart, ResponsiveContainer, YAxis } from "recharts";
import { Card } from "./ui/card";
import { cn } from "../lib/utils";

type Series = { t: number; v: number }[];

interface Props {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  icon?: ReactNode;
  percent?: number;
  series?: Series;
  tone?: "auto" | "good" | "warn" | "bad" | "info";
  yMax?: number;
}

function toneFromPercent(p?: number): "good" | "warn" | "bad" | "info" {
  if (p === undefined) return "info";
  if (p >= 90) return "bad";
  if (p >= 70) return "warn";
  return "good";
}

const toneColor: Record<string, string> = {
  good: "rgb(34 197 94)",
  warn: "rgb(245 158 11)",
  bad: "rgb(239 68 68)",
  info: "rgb(59 130 246)",
};

export function MetricCard({ label, value, sub, icon, percent, series, tone = "auto", yMax }: Props) {
  const resolved = tone === "auto" ? toneFromPercent(percent) : tone;
  const color = toneColor[resolved];
  const gid = `grad-${label.replace(/\s/g, "-")}`;

  return (
    <Card className="relative overflow-hidden">
      <div className="p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {icon}
            {label}
          </div>
        </div>
        <div className="mt-2 text-2xl font-semibold font-mono tabular-nums tracking-tight">{value}</div>
        {sub !== undefined && <div className="mt-0.5 text-xs text-muted-foreground">{sub}</div>}
        {percent !== undefined && (
          <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-secondary">
            <div
              className={cn("h-full transition-all", `bg-[${color}]`)}
              style={{ width: `${Math.max(0, Math.min(100, percent))}%`, backgroundColor: color }}
            />
          </div>
        )}
      </div>
      {series && series.length > 1 && (
        <div className="h-12">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={series} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={color} stopOpacity={0.35} />
                  <stop offset="100%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              </defs>
              <YAxis domain={[0, yMax ?? "auto"]} hide />
              <Area type="monotone" dataKey="v" stroke={color} strokeWidth={1.5} fill={`url(#${gid})`} isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  );
}
