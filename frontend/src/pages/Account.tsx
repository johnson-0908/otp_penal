import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { KeyRound, ShieldCheck, ShieldAlert, Copy, Check } from "lucide-react";
import { api, ApiError, isDevMode } from "../api";
import { useAuth } from "../auth";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Badge } from "../components/ui/badge";

export default function Account() {
  const { me, refreshMe } = useAuth();
  const nav = useNavigate();
  const devMode = isDevMode();

  if (!me) return null;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">账号</h1>
        <p className="mt-1 text-sm text-muted-foreground">当前用户：{me.username}</p>
      </div>

      <ChangePasswordCard
        mustChange={me.must_change_password}
        hasTotp={me.has_totp}
        devMode={devMode}
        onSuccess={async () => {
          await refreshMe();
          if (me.must_change_password) nav("/", { replace: true });
        }}
      />

      {!devMode && (
        <TotpCard hasTotp={me.has_totp} onChanged={refreshMe} />
      )}
    </div>
  );
}

function ChangePasswordCard({
  mustChange,
  hasTotp,
  devMode,
  onSuccess,
}: {
  mustChange: boolean;
  hasTotp: boolean;
  devMode: boolean;
  onSuccess: () => Promise<void>;
}) {
  const [oldPw, setOld] = useState("");
  const [newPw, setNew] = useState("");
  const [confirm, setConfirm] = useState("");
  const [code, setCode] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const needsCode = !devMode && hasTotp;

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
      await onSuccess();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "修改失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="h-4 w-4" /> 修改密码
        </CardTitle>
        <CardDescription>
          {mustChange ? "首次登录必须修改初始密码。" : "新密码至少 12 位。"}
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
          {needsCode && (
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
  );
}

function TotpCard({ hasTotp, onChanged }: { hasTotp: boolean; onChanged: () => Promise<void> }) {
  return (
    <Card className="max-w-xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          {hasTotp ? <ShieldCheck className="h-4 w-4 text-emerald-500" /> : <ShieldAlert className="h-4 w-4 text-amber-500" />}
          Authenticator 双因素
          {hasTotp ? (
            <Badge variant="success" className="ml-1">已绑定</Badge>
          ) : (
            <Badge variant="warning" className="ml-1">未绑定</Badge>
          )}
        </CardTitle>
        <CardDescription>
          {hasTotp
            ? "已启用 TOTP 双因素。下次登录需要输入 6 位动态验证码。"
            : "建议绑定。绑定后登录需要密码 + 6 位动态验证码。"}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {hasTotp ? <UnbindForm onChanged={onChanged} /> : <BindForm onChanged={onChanged} />}
      </CardContent>
    </Card>
  );
}

function BindForm({ onChanged }: { onChanged: () => Promise<void> }) {
  const [setup, setSetup] = useState<{ secret: string; otpauth_url: string; qr_png_base64: string } | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  async function beginSetup() {
    setErr(null);
    setOk(null);
    setLoading(true);
    try {
      const data = await api<{ secret: string; otpauth_url: string; qr_png_base64: string }>(
        "/api/account/totp/setup",
        { method: "POST" },
      );
      setSetup(data);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "生成失败");
    } finally {
      setLoading(false);
    }
  }

  async function onConfirm(e: FormEvent) {
    e.preventDefault();
    if (!setup) return;
    setErr(null);
    setLoading(true);
    try {
      await api("/api/account/totp/confirm", {
        method: "POST",
        body: { secret: setup.secret, code: code.trim(), password },
      });
      setOk("绑定成功");
      setSetup(null);
      setCode("");
      setPassword("");
      await onChanged();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "绑定失败");
    } finally {
      setLoading(false);
    }
  }

  function copySecret() {
    if (!setup) return;
    navigator.clipboard.writeText(setup.secret).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }

  if (!setup) {
    return (
      <div className="space-y-3">
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
        <Button onClick={beginSetup} disabled={loading}>
          {loading ? "生成中..." : "开始绑定"}
        </Button>
      </div>
    );
  }

  return (
    <form onSubmit={onConfirm} className="space-y-4">
      <div className="rounded-md border bg-muted/30 p-4 space-y-3">
        <p className="text-sm text-muted-foreground">
          1. 用 Authenticator app（Google Authenticator / Authy / 1Password / Aegis）扫描二维码，
          或手动输入下方密钥。
        </p>
        <div className="flex items-start gap-4">
          <img
            src={`data:image/png;base64,${setup.qr_png_base64}`}
            alt="TOTP QR code"
            className="h-40 w-40 rounded-md border bg-white p-2"
          />
          <div className="min-w-0 flex-1 space-y-2">
            <div>
              <div className="text-xs text-muted-foreground mb-1">手动输入密钥（base32）</div>
              <div className="flex items-center gap-1">
                <code className="flex-1 truncate rounded bg-background px-2 py-1.5 text-xs font-mono border">
                  {setup.secret}
                </code>
                <Button type="button" variant="ghost" size="icon" onClick={copySecret} title="复制">
                  {copied ? <Check className="h-4 w-4 text-emerald-500" /> : <Copy className="h-4 w-4" />}
                </Button>
              </div>
            </div>
            <p className="text-[11px] text-muted-foreground leading-4">
              若扫码失败：用 app 的"手动输入"方式，账户名随意，密钥粘贴上面那串。
            </p>
          </div>
        </div>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="bind-code">2. 输入 app 生成的 6 位验证码</Label>
        <Input
          id="bind-code"
          inputMode="numeric"
          pattern="[0-9]{6}"
          maxLength={6}
          value={code}
          onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
          className="text-center font-mono tracking-widest"
          required
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="bind-pw">3. 输入当前账号密码确认</Label>
        <Input
          id="bind-pw"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          className="font-mono"
          required
        />
      </div>

      {err && (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {err}
        </div>
      )}

      <div className="flex gap-2">
        <Button type="submit" disabled={loading}>
          {loading ? "绑定中..." : "确认绑定"}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            setSetup(null);
            setCode("");
            setPassword("");
            setErr(null);
          }}
        >
          取消
        </Button>
      </div>
    </form>
  );
}

function UnbindForm({ onChanged }: { onChanged: () => Promise<void> }) {
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    setLoading(true);
    try {
      await api("/api/account/totp/disable", {
        method: "POST",
        body: { password, code: code.trim() },
      });
      setOk("已解绑");
      setPassword("");
      setCode("");
      setShowForm(false);
      await onChanged();
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "解绑失败");
    } finally {
      setLoading(false);
    }
  }

  if (!showForm) {
    return (
      <div className="space-y-3">
        {ok && (
          <div className="rounded-md border border-emerald-600/30 bg-emerald-600/10 px-3 py-2 text-sm text-emerald-400">
            {ok}
          </div>
        )}
        <Button variant="destructive" onClick={() => setShowForm(true)}>
          解绑 Authenticator
        </Button>
        <p className="text-[11px] leading-4 text-muted-foreground">
          解绑后下次登录只需密码，安全性下降。除非更换设备，否则不建议操作。
        </p>
      </div>
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      <div className="space-y-1.5">
        <Label htmlFor="unbind-pw">账号密码</Label>
        <Input
          id="unbind-pw"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          className="font-mono"
          required
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="unbind-code">当前 Authenticator 6 位验证码</Label>
        <Input
          id="unbind-code"
          inputMode="numeric"
          pattern="[0-9]{6}"
          maxLength={6}
          value={code}
          onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
          className="text-center font-mono tracking-widest"
          required
        />
      </div>
      {err && (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {err}
        </div>
      )}
      <div className="flex gap-2">
        <Button type="submit" variant="destructive" disabled={loading}>
          {loading ? "解绑中..." : "确认解绑"}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            setShowForm(false);
            setErr(null);
          }}
        >
          取消
        </Button>
      </div>
    </form>
  );
}
