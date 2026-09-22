"use client";

import { createContext, FormEvent, ReactNode, useContext, useEffect, useState, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import { ApiClient, apiClient, Organization, Role, User } from "./api";
import { browserLogoutStorage, SessionController } from "./session-controller";

type SessionStatus = "loading" | "authenticated" | "signed_out";
type AuthMode = "login" | "register";

type SessionContextValue = {
  status: SessionStatus;
  user: User | null;
  accessToken: string | null;
  organizations: Organization[];
  organization: Organization | null;
  sessionNotice: string;
  client: ApiClient;
  signIn: (mode: AuthMode, values: Record<string, string>) => Promise<void>;
  signOut: () => Promise<void>;
  selectOrganization: (organizationID: string) => void;
};

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [controller] = useState(() => new SessionController(apiClient, browserLogoutStorage));
  const snapshot = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getServerSnapshot);
  useEffect(() => {
    window.addEventListener("storage", controller.onStorage);
    const cleanup = controller.restore();
    return () => { cleanup(); window.removeEventListener("storage", controller.onStorage); };
  }, [controller]);
  return <SessionContext.Provider value={{ ...snapshot, client: apiClient, signIn: controller.signIn, signOut: controller.signOut, selectOrganization: controller.selectOrganization }}>{children}</SessionContext.Provider>;
}
export function useSession() {
  const context = useContext(SessionContext);
  if (!context) throw new Error("useSession must be used inside SessionProvider");
  return context;
}

export function canOperate(role: Role | undefined) {
  return role === "owner" || role === "admin" || role === "warehouse_manager";
}

export function AuthScreen() {
  const { signIn, sessionNotice } = useSession();
  const router = useRouter();
  const [mode, setMode] = useState<AuthMode>("login");
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(false);
  const registrationEnabled = process.env.NEXT_PUBLIC_APP_ENV !== "production";

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setMessage("");
    const form = new FormData(event.currentTarget);
    const values: Record<string, string> = { email: String(form.get("email") ?? ""), password: String(form.get("password") ?? "") };
    if (mode === "register") {
      values.first_name = String(form.get("first_name") ?? "");
      values.last_name = String(form.get("last_name") ?? "");
    }
    try {
      await signIn(mode, values);
      router.push("/dashboard");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Unable to sign in.");
    } finally {
      setLoading(false);
    }
  }

  return <main className="auth-page"><section className="auth-intro"><p className="eyebrow">FlowCart OS</p><h1>Operations, in one clear view.</h1><p>Coordinate stock, orders, and replenishment across every warehouse.</p></section><section className="auth-panel" aria-labelledby="auth-heading"><div className="auth-tabs"><button className={mode === "login" ? "active" : ""} onClick={() => setMode("login")}>Sign in</button>{registrationEnabled && <button className={mode === "register" ? "active" : ""} onClick={() => setMode("register")}>Create account</button>}</div><h2 id="auth-heading">{mode === "login" ? "Welcome back" : "Create your account"}</h2>{sessionNotice && <p className="form-message" role="status">{sessionNotice}</p>}{message && <p className="form-message error" role="alert">{message}</p>}<form onSubmit={submit} className="stack-form"><label>Email<input name="email" type="email" autoComplete="email" required /></label><label>Password<input name="password" type="password" autoComplete={mode === "login" ? "current-password" : "new-password"} minLength={8} required /></label>{mode === "register" && registrationEnabled && <div className="form-grid"><label>First name<input name="first_name" required /></label><label>Last name<input name="last_name" required /></label></div>}<button className="primary-button" disabled={loading}>{loading ? "Working..." : mode === "login" ? "Sign in" : "Create account"}</button></form></section></main>;
}
