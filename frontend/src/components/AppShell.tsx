import { ReactNode } from "react";
import { NavLink, useNavigate } from "react-router-dom";
import { Activity, ClipboardList, UserCog, LogOut, Server } from "lucide-react";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Separator } from "./ui/separator";
import { ThemeToggle } from "./ThemeToggle";
import { useAuth } from "../auth";
import { isDevMode } from "../api";
import { cn } from "../lib/utils";

const items = [
  { to: "/", label: "性能监控", icon: Activity, end: true },
  { to: "/audit", label: "审计日志", icon: ClipboardList, end: false },
  { to: "/account", label: "账号", icon: UserCog, end: false },
];

export function AppShell({ children }: { children: ReactNode }) {
  const { me, logout } = useAuth();
  const nav = useNavigate();
  const dev = isDevMode();

  async function handleLogout() {
    await logout();
    nav("/login", { replace: true });
  }

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
      <aside className="flex w-60 shrink-0 flex-col border-r bg-card">
        <div className="flex h-14 items-center gap-2 px-4">
          <div className="flex h-7 w-7 items-center justify-center rounded-md bg-primary/10 text-primary">
            <Server className="h-4 w-4" />
          </div>
          <div className="flex-1">
            <div className="text-sm font-semibold leading-tight">Ops Panel</div>
            <div className="text-[10px] leading-tight text-muted-foreground">v0.1 · self-hosted</div>
          </div>
          {dev && <Badge variant="warning" className="text-[10px]">DEV</Badge>}
        </div>
        <Separator />
        <nav className="flex-1 space-y-0.5 p-2">
          {items.map((i) => (
            <NavLink
              key={i.to}
              to={i.to}
              end={i.end}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground",
                )
              }
            >
              <i.icon className="h-4 w-4" />
              {i.label}
            </NavLink>
          ))}
        </nav>
        <Separator />
        <div className="p-3 space-y-2">
          <div className="flex justify-center">
            <ThemeToggle />
          </div>
          <div className="flex items-center gap-2">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/15 text-xs font-semibold uppercase text-primary">
              {me?.username?.[0] ?? "?"}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{me?.username ?? "—"}</div>
              <div className="truncate text-[11px] text-muted-foreground">已登录</div>
            </div>
            <Button variant="ghost" size="icon" onClick={handleLogout} title="退出">
              <LogOut className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </aside>
      <main className="flex-1 overflow-auto">
        <div className="mx-auto max-w-[1400px] p-6">{children}</div>
      </main>
    </div>
  );
}
