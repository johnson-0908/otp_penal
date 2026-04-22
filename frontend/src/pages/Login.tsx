import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Server } from "lucide-react";
import { useAuth } from "../auth";
import { ApiError, isDevMode, prepareCsrf } from "../api";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Badge } from "../components/ui/badge";

export default function Login() {
  const { login, me } = useAuth();
  const nav = useNavigate();
  const [username, setU] = useState("admin");
  const [password, setP] = useState("");
  const [code, setC] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [devMode, setDevMode] = useState(false);

  useEffect(() => {
    prepareCsrf().then(() => setDevMode(isDevMode()));
  }, []);

  useEffect(() => {
    if (me) {
      nav(me.must_change_password ? "/account" : "/", { replace: true });
    }
  }, [me, nav]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    setLoading(true);
    try {
      await login(username.trim(), password, code.trim());
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "登录失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-2">
          <div className="flex items-center gap-2">
            <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary/10 text-primary">
              <Server className="h-5 w-5" />
            </div>
            <div className="flex-1">
              <CardTitle className="text-lg">Ops Panel</CardTitle>
              <CardDescription className="text-xs">
                {devMode ? "DEV 模式 · admin / admin · TOTP 已禁用" : "密码 + 动态验证码（TOTP）"}
              </CardDescription>
            </div>
            {devMode && <Badge variant="warning">DEV</Badge>}
          </div>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} autoComplete="off" className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="username">用户名</Label>
              <Input
                id="username"
                value={username}
                onChange={(e) => setU(e.target.value)}
                autoCapitalize="off"
                autoCorrect="off"
                spellCheck={false}
                className="font-mono"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="password">密码</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setP(e.target.value)}
                autoComplete="current-password"
                className="font-mono"
                required
              />
            </div>
            {!devMode && (
              <div className="space-y-1.5">
                <Label htmlFor="code">动态验证码 (6 位)</Label>
                <Input
                  id="code"
                  inputMode="numeric"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  value={code}
                  onChange={(e) => setC(e.target.value.replace(/\D/g, "").slice(0, 6))}
                  autoComplete="one-time-code"
                  className="text-center font-mono tracking-widest"
                  required
                />
              </div>
            )}
            {err && (
              <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {err}
              </div>
            )}
            <Button type="submit" disabled={loading} className="w-full">
              {loading ? "登录中..." : "登录"}
            </Button>
            <p className="text-[11px] leading-4 text-muted-foreground">
              提示：本面板默认只监听 127.0.0.1。生产环境请走 Tailscale / WireGuard / Cloudflare Tunnel 访问。
            </p>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
