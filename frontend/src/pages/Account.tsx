import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { KeyRound } from "lucide-react";
import { api, ApiError, isDevMode } from "../api";
import { useAuth } from "../auth";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";

export default function Account() {
  const { me, refreshMe } = useAuth();
  const nav = useNavigate();
  const [oldPw, setOld] = useState("");
  const [newPw, setNew] = useState("");
  const [confirm, setConfirm] = useState("");
  const [code, setCode] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const devMode = isDevMode();

  if (!me) return null;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    setOk(null);
    if (newPw.length < 12) {
      setErr("新密码至少 12 位");
      return;
    }
    if (newPw !== confirm) {
      setErr("两次密码不一致");
      return;
    }
    setLoading(true);
    try {
      await api("/api/auth/change-password", {
        method: "POST",
        body: { old_password: oldPw, new_password: newPw, code: code.trim() },
      });
      setOk("密码已更新");
      setOld("");
      setNew("");
      setConfirm("");
      setCode("");
      await refreshMe();
      if (me?.must_change_password) {
        nav("/", { replace: true });
      }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "修改失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">账号</h1>
        <p className="mt-1 text-sm text-muted-foreground">当前用户：{me.username}</p>
      </div>

      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <KeyRound className="h-4 w-4" /> 修改密码
          </CardTitle>
          <CardDescription>
            {me.must_change_password ? "首次登录必须修改初始密码。" : "新密码至少 12 位。"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="old">当前密码</Label>
              <Input
                id="old"
                type="password"
                value={oldPw}
                onChange={(e) => setOld(e.target.value)}
                autoComplete="current-password"
                className="font-mono"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="new">新密码 (≥12 位)</Label>
              <Input
                id="new"
                type="password"
                value={newPw}
                onChange={(e) => setNew(e.target.value)}
                minLength={12}
                autoComplete="new-password"
                className="font-mono"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="confirm">确认新密码</Label>
              <Input
                id="confirm"
                type="password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                minLength={12}
                autoComplete="new-password"
                className="font-mono"
                required
              />
            </div>
            {!devMode && (
              <div className="space-y-1.5">
                <Label htmlFor="code">动态验证码</Label>
                <Input
                  id="code"
                  inputMode="numeric"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
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
            {ok && (
              <div className="rounded-md border border-emerald-600/30 bg-emerald-600/10 px-3 py-2 text-sm text-emerald-400">
                {ok}
              </div>
            )}
            <Button type="submit" disabled={loading}>
              {loading ? "提交中..." : "提交"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
