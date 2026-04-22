import { useEffect, useRef, useState } from "react";

export type Sample = {
  t: number;
  cpu: number;
  mem: number;
  swap: number;
  netTxRate: number; // bytes/sec, -1 for unavailable
  netRxRate: number;
  diskReadRate: number;
  diskWriteRate: number;
};

type Raw = {
  sampled_at: string;
  cpu: { percent_total: number };
  memory: { percent: number };
  swap: { percent: number };
  network: { bytes_sent: number; bytes_recv: number };
  disk_io: { read_bytes: number; write_bytes: number };
};

const MAX = 60;

export function useMetricsHistory(data: Raw | undefined | null) {
  const [history, setHistory] = useState<Sample[]>([]);
  const prev = useRef<{ t: number; tx: number; rx: number; diskR: number; diskW: number } | null>(null);

  useEffect(() => {
    if (!data) return;
    const t = new Date(data.sampled_at).getTime() || Date.now();
    let netTxRate = -1;
    let netRxRate = -1;
    let diskReadRate = -1;
    let diskWriteRate = -1;
    if (prev.current) {
      const dt = (t - prev.current.t) / 1000;
      if (dt > 0) {
        netTxRate = Math.max(0, (data.network.bytes_sent - prev.current.tx) / dt);
        netRxRate = Math.max(0, (data.network.bytes_recv - prev.current.rx) / dt);
        diskReadRate = Math.max(0, (data.disk_io.read_bytes - prev.current.diskR) / dt);
        diskWriteRate = Math.max(0, (data.disk_io.write_bytes - prev.current.diskW) / dt);
      }
    }
    prev.current = {
      t,
      tx: data.network.bytes_sent,
      rx: data.network.bytes_recv,
      diskR: data.disk_io.read_bytes,
      diskW: data.disk_io.write_bytes,
    };
    setHistory((h) => {
      const next = [
        ...h,
        {
          t,
          cpu: data.cpu.percent_total,
          mem: data.memory.percent,
          swap: data.swap.percent,
          netTxRate,
          netRxRate,
          diskReadRate,
          diskWriteRate,
        },
      ];
      return next.length > MAX ? next.slice(next.length - MAX) : next;
    });
  }, [data]);

  return history;
}
