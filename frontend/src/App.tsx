import { Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "./auth";
import { AppShell } from "./components/AppShell";
import Login from "./pages/Login";
import Monitor from "./pages/Monitor";
import Audit from "./pages/Audit";
import Account from "./pages/Account";

function Shell({ children }: { children: JSX.Element }) {
  const { me, loading } = useAuth();
  if (loading) return <div className="flex h-screen items-center justify-center text-muted-foreground text-sm">加载中…</div>;
  if (!me) return <Navigate to="/login" replace />;
  if (me.must_change_password && window.location.pathname !== "/account") {
    return <Navigate to="/account" replace />;
  }
  return <AppShell>{children}</AppShell>;
}

function Redirector() {
  const { me, loading } = useAuth();
  if (loading) return null;
  return <Navigate to={me ? "/" : "/login"} replace />;
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<Shell><Monitor /></Shell>} />
        <Route path="/audit" element={<Shell><Audit /></Shell>} />
        <Route path="/account" element={<Shell><Account /></Shell>} />
        <Route path="/change-password" element={<Navigate to="/account" replace />} />
        <Route path="*" element={<Redirector />} />
      </Routes>
    </AuthProvider>
  );
}
