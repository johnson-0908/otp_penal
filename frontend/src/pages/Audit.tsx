import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, ClipboardList, ShieldAlert } from "lucide-react";
import { api } from "../api";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Badge } from "../components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";

type AuditEntry = {
  ID: number;
  UserID: { Int64: number; Valid: boolean };
  IP: string;
  Action: string;
  Detail: string;
  CreatedAt: string;
  PrevHash: string;
  Hash: string;
};

type Attempt = {
  IP: string;
  Username: string;
  Success: boolean;
  Reason: string;
  CreatedAt: string;
};

type AttemptsPage = {
  items: Attempt[];
  total: number;
};

const ATTEMPTS_PAGE_SIZE = 20;

export default function Audit() {
  const audit = useQuery<AuditEntry[]>({
    queryKey: ["audit"],
    queryFn: () => api<AuditEntry[]>("/api/audit?limit=100"),
    refetchInterval: 10_000,
  });

  const [attemptsPage, setAttemptsPage] = useState(1);
  const attemptsOffset = (attemptsPage - 1) * ATTEMPTS_PAGE_SIZE;
  const attempts = useQuery<AttemptsPage>({
    queryKey: ["attempts", attemptsPage],
    queryFn: () =>
      api<AttemptsPage>(
        `/api/security/recent-attempts?limit=${ATTEMPTS_PAGE_SIZE}&offset=${attemptsOffset}`,
      ),
    refetchInterval: 10_000,
    placeholderData: keepPreviousData,
  });

  const attemptsTotal = attempts.data?.total ?? 0;
  const attemptsItems = attempts.data?.items ?? [];
  const attemptsTotalPages = Math.max(1, Math.ceil(attemptsTotal / ATTEMPTS_PAGE_SIZE));
  const attemptsRangeStart = attemptsTotal === 0 ? 0 : attemptsOffset + 1;
  const attemptsRangeEnd = Math.min(attemptsOffset + attemptsItems.length, attemptsTotal);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">审计日志</h1>
        <p className="mt-1 text-sm text-muted-foreground">登录尝试 · 关键操作 (append-only + SHA-256 hash chain)</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <ShieldAlert className="h-4 w-4" /> 最近登录尝试
          </CardTitle>
          <CardDescription>共 {attemptsTotal} 条 · 每页 {ATTEMPTS_PAGE_SIZE} 条</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>IP</TableHead>
                <TableHead>用户名</TableHead>
                <TableHead>结果</TableHead>
                <TableHead>原因</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {attemptsItems.map((a, i) => (
                <TableRow key={`${a.CreatedAt}-${i}`}>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {new Date(a.CreatedAt).toLocaleString()}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{a.IP}</TableCell>
                  <TableCell className="font-mono text-xs">{a.Username}</TableCell>
                  <TableCell>
                    {a.Success ? <Badge variant="success">成功</Badge> : <Badge variant="danger">失败</Badge>}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{a.Reason || "—"}</TableCell>
                </TableRow>
              ))}
              {attemptsItems.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="py-8 text-center text-sm text-muted-foreground">
                    暂无记录
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
        {attemptsTotal > 0 && (
          <div className="flex items-center justify-between border-t px-4 py-3 text-xs text-muted-foreground">
            <span>
              第 {attemptsRangeStart}-{attemptsRangeEnd} 条 / 共 {attemptsTotal} 条
            </span>
            <div className="flex items-center gap-2">
              <span>
                第 {attemptsPage} / {attemptsTotalPages} 页
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={attemptsPage <= 1}
                onClick={() => setAttemptsPage((p) => Math.max(1, p - 1))}
              >
                <ChevronLeft /> 上一页
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={attemptsPage >= attemptsTotalPages}
                onClick={() => setAttemptsPage((p) => Math.min(attemptsTotalPages, p + 1))}
              >
                下一页 <ChevronRight />
              </Button>
            </div>
          </div>
        )}
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <ClipboardList className="h-4 w-4" /> 审计日志
          </CardTitle>
          <CardDescription>append-only · 每条都链到上一条的 hash</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>时间</TableHead>
                <TableHead>IP</TableHead>
                <TableHead>动作</TableHead>
                <TableHead>详情</TableHead>
                <TableHead>hash</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(audit.data ?? []).map((e) => (
                <TableRow key={e.ID}>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {new Date(e.CreatedAt).toLocaleString()}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{e.IP || "—"}</TableCell>
                  <TableCell>
                    <span className="font-mono text-xs text-primary">{e.Action}</span>
                  </TableCell>
                  <TableCell className="text-xs">{e.Detail || "—"}</TableCell>
                  <TableCell className="max-w-[10rem] truncate font-mono text-xs text-muted-foreground">
                    {e.Hash.slice(0, 12)}…
                  </TableCell>
                </TableRow>
              ))}
              {audit.data?.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="py-8 text-center text-sm text-muted-foreground">
                    暂无记录
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
