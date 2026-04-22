import React, { createContext, useContext, useEffect, useState } from "react";
import { api, clearTokens, hasRefreshToken, setTokens } from "./api";

type Me = {
  id: number;
  username: string;
  created_at: string;
  must_change_password: boolean;
};

type Ctx = {
  me: Me | null;
  loading: boolean;
  login: (u: string, p: string, code: string) => Promise<void>;
  logout: () => Promise<void>;
  refreshMe: () => Promise<void>;
};

const AuthContext = createContext<Ctx | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);

  const refreshMe = async () => {
    try {
      const m = await api<Me>("/api/me");
      setMe(m);
    } catch {
      setMe(null);
    }
  };

  useEffect(() => {
    (async () => {
      if (hasRefreshToken()) await refreshMe();
      setLoading(false);
    })();
  }, []);

  const login: Ctx["login"] = async (username, password, code) => {
    const data = await api<{
      access_token: string;
      refresh_token: string;
      access_expires_at: string;
      refresh_expires_at: string;
      must_change_password: boolean;
    }>("/api/auth/login", {
      method: "POST",
      auth: false,
      body: { username, password, code },
    });
    setTokens(data.access_token, data.access_expires_at, data.refresh_token);
    await refreshMe();
  };

  const logout = async () => {
    try {
      await api("/api/auth/logout", { method: "POST" });
    } catch {
      /* ignore */
    } finally {
      clearTokens();
      setMe(null);
    }
  };

  return (
    <AuthContext.Provider value={{ me, loading, login, logout, refreshMe }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const c = useContext(AuthContext);
  if (!c) throw new Error("useAuth outside provider");
  return c;
}
