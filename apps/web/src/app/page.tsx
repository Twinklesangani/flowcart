"use client";

import { FormEvent, useEffect, useState } from "react";

const apiUrl = process.env.NEXT_PUBLIC_API_URL ?? "";
type User = { id: string; email: string; first_name: string; last_name: string };
type Mode = "login" | "register";

async function apiRequest(path: string, options: RequestInit = {}) {
  return fetch(`${apiUrl}${path}`, {
    ...options,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...options.headers },
  });
}

export default function Home() {
  const [mode, setMode] = useState<Mode>("login");
  const [user, setUser] = useState<User | null>(null);
  const [, setAccessToken] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [message, setMessage] = useState(apiUrl ? "Checking your session..." : "API URL is not configured.");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!apiUrl) {
      return;
    }

    apiRequest("/api/v1/auth/refresh", { method: "POST" })
      .then(async (response) => {
        if (!response.ok) throw new Error("No active session");
        const result = (await response.json()) as { access_token: string };
        setAccessToken(result.access_token);
        const meResponse = await apiRequest("/api/v1/auth/me", {
          headers: { Authorization: `Bearer ${result.access_token}` },
        });
        if (!meResponse.ok) throw new Error("Session expired");
        setUser((await meResponse.json()) as User);
        setMessage("");
      })
      .catch(() => setMessage("Sign in to continue."));
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setMessage("");
    const path = mode === "login" ? "/api/v1/auth/login" : "/api/v1/auth/register";
    const body = mode === "login"
      ? { email, password }
      : { email, password, first_name: firstName, last_name: lastName };
    try {
      const response = await apiRequest(path, { method: "POST", body: JSON.stringify(body) });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error?.message ?? "Request failed.");
      setAccessToken(result.access_token);
      setUser(result.user);
      setPassword("");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Request failed.");
    } finally {
      setLoading(false);
    }
  }

  async function logout() {
    await apiRequest("/api/v1/auth/logout", { method: "POST" });
    setAccessToken(null);
    setUser(null);
    setMessage("You have been signed out.");
  }

  if (user) {
    return (
      <main className="mx-auto flex min-h-screen w-full max-w-3xl flex-col justify-center px-6 py-16">
        <p className="text-sm font-semibold uppercase tracking-[0.2em] text-teal-700">FlowCart OS</p>
        <h1 className="mt-5 text-4xl font-semibold tracking-tight">Welcome back.</h1>
        <p className="mt-4 text-lg text-slate-600">{user.email}</p>
        <p className="mt-2 text-slate-600">Your authenticated session is active.</p>
        <button className="mt-8 w-fit bg-slate-950 px-5 py-3 text-sm font-semibold text-white hover:bg-teal-800" onClick={logout}>Sign out</button>
      </main>
    );
  }

  return (
    <main className="min-h-screen bg-slate-100 px-6 py-12 text-slate-950 sm:px-10">
      <div className="mx-auto grid w-full max-w-5xl gap-12 lg:grid-cols-[1fr_380px] lg:items-center">
        <section>
          <p className="text-sm font-semibold uppercase tracking-[0.2em] text-teal-700">FlowCart OS</p>
          <h1 className="mt-5 max-w-xl text-4xl font-semibold tracking-tight sm:text-6xl">Multi-Warehouse Order &amp; Fulfilment Platform</h1>
          <p className="mt-6 max-w-lg text-lg leading-8 text-slate-600">A focused operating foundation for coordinating orders and fulfilment across every warehouse.</p>
        </section>
        <section className="bg-white p-7 shadow-sm" aria-labelledby="auth-heading">
          <div className="flex gap-6 border-b border-slate-200">
            <button className={`pb-3 text-sm font-semibold ${mode === "login" ? "border-b-2 border-teal-700 text-teal-800" : "text-slate-500"}`} onClick={() => setMode("login")}>Sign in</button>
            <button className={`pb-3 text-sm font-semibold ${mode === "register" ? "border-b-2 border-teal-700 text-teal-800" : "text-slate-500"}`} onClick={() => setMode("register")}>Create account</button>
          </div>
          <h2 id="auth-heading" className="mt-7 text-2xl font-semibold">{mode === "login" ? "Sign in" : "Create your account"}</h2>
          {message && <p className="mt-4 text-sm text-slate-600" role="status">{message}</p>}
          <form className="mt-6 space-y-4" onSubmit={submit}>
            <label className="block text-sm font-medium">Email<input className="mt-1 w-full border border-slate-300 px-3 py-2.5 outline-none focus:border-teal-700" type="email" required value={email} onChange={(event) => setEmail(event.target.value)} /></label>
            <label className="block text-sm font-medium">Password<input className="mt-1 w-full border border-slate-300 px-3 py-2.5 outline-none focus:border-teal-700" type="password" minLength={8} required value={password} onChange={(event) => setPassword(event.target.value)} /></label>
            {mode === "register" && <><label className="block text-sm font-medium">First name<input className="mt-1 w-full border border-slate-300 px-3 py-2.5 outline-none focus:border-teal-700" value={firstName} onChange={(event) => setFirstName(event.target.value)} /></label><label className="block text-sm font-medium">Last name<input className="mt-1 w-full border border-slate-300 px-3 py-2.5 outline-none focus:border-teal-700" value={lastName} onChange={(event) => setLastName(event.target.value)} /></label></>}
            <button className="w-full bg-slate-950 px-4 py-3 font-semibold text-white hover:bg-teal-800 disabled:cursor-not-allowed disabled:opacity-60" disabled={loading || !apiUrl}>{loading ? "Please wait..." : mode === "login" ? "Sign in" : "Create account"}</button>
          </form>
        </section>
      </div>
    </main>
  );
}
