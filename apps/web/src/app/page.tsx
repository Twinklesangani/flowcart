"use client";

import { FormEvent, useEffect, useState } from "react";

const apiUrl = process.env.NEXT_PUBLIC_API_URL ?? "";
type Role = "owner" | "admin" | "warehouse_manager" | "support" | "viewer";
type User = { id: string; email: string; first_name: string; last_name: string };
type Organization = { id: string; name: string; slug: string; role: Role };
type Member = User & { role: Role; created_at: string };
type Mode = "login" | "register";

async function apiRequest(path: string, token: string | null, options: RequestInit = {}) {
  return fetch(`${apiUrl}${path}`, {
    ...options,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}), ...options.headers },
  });
}

export default function Home() {
  const [mode, setMode] = useState<Mode>("login");
  const [user, setUser] = useState<User | null>(null);
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [selectedOrganization, setSelectedOrganization] = useState<Organization | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [organizationName, setOrganizationName] = useState("");
  const [organizationSlug, setOrganizationSlug] = useState("");
  const [memberEmail, setMemberEmail] = useState("");
  const [memberRole, setMemberRole] = useState<Role>("viewer");
  const [message, setMessage] = useState(apiUrl ? "Checking your session..." : "API URL is not configured.");
  const [loading, setLoading] = useState(false);

  async function loadOrganizations(token: string) {
    const response = await apiRequest("/api/v1/organizations", token);
    if (!response.ok) throw new Error("Unable to load organizations.");
    const result = (await response.json()) as { organizations: Organization[] };
    setOrganizations(result.organizations);
    setSelectedOrganization(result.organizations[0] ?? null);
  }

  async function loadMembers(token: string, organization: Organization) {
    const response = await apiRequest(`/api/v1/organizations/${organization.id}/members`, token);
    if (!response.ok) throw new Error("Unable to load members.");
    return ((await response.json()) as { members: Member[] }).members;
  }

  useEffect(() => {
    if (!apiUrl) return;
    apiRequest("/api/v1/auth/refresh", null, { method: "POST" })
      .then(async (response) => {
        if (!response.ok) throw new Error("No active session");
        const result = (await response.json()) as { access_token: string };
        const meResponse = await apiRequest("/api/v1/auth/me", result.access_token);
        if (!meResponse.ok) throw new Error("Session expired");
        setAccessToken(result.access_token);
        setUser((await meResponse.json()) as User);
        await loadOrganizations(result.access_token);
        setMessage("");
      })
      .catch(() => setMessage("Sign in to continue."));
  }, []);

  useEffect(() => {
    if (accessToken && selectedOrganization) {
      loadMembers(accessToken, selectedOrganization)
        .then((loadedMembers) => setMembers(loadedMembers))
        .catch((error: Error) => setMessage(error.message));
    }
  }, [accessToken, selectedOrganization]);

  async function submitAuth(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setLoading(true); setMessage("");
    const path = mode === "login" ? "/api/v1/auth/login" : "/api/v1/auth/register";
    const body = mode === "login" ? { email, password } : { email, password, first_name: firstName, last_name: lastName };
    try {
      const response = await apiRequest(path, null, { method: "POST", body: JSON.stringify(body) });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error?.message ?? "Request failed.");
      setAccessToken(result.access_token); setUser(result.user); await loadOrganizations(result.access_token); setPassword("");
    } catch (error) { setMessage(error instanceof Error ? error.message : "Request failed."); } finally { setLoading(false); }
  }

  async function createOrganization(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!accessToken) return; setLoading(true);
    try { const response = await apiRequest("/api/v1/organizations", accessToken, { method: "POST", body: JSON.stringify({ name: organizationName, slug: organizationSlug }) }); const created = await response.json(); if (!response.ok) throw new Error(created.error?.message ?? "Unable to create organization."); await loadOrganizations(accessToken); setOrganizationName(""); setOrganizationSlug(""); setMessage("Organization created."); } catch (error) { setMessage(error instanceof Error ? error.message : "Request failed."); } finally { setLoading(false); }
  }

  async function addMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!accessToken || !selectedOrganization) return; const response = await apiRequest(`/api/v1/organizations/${selectedOrganization.id}/members`, accessToken, { method: "POST", body: JSON.stringify({ email: memberEmail, role: memberRole }) }); if (!response.ok) { const result = await response.json(); setMessage(result.error?.message ?? "Unable to add member."); return; } setMemberEmail(""); setMembers(await loadMembers(accessToken, selectedOrganization));
  }

  async function changeRole(member: Member, role: Role) { if (!accessToken || !selectedOrganization) return; const response = await apiRequest(`/api/v1/organizations/${selectedOrganization.id}/members/${member.id}`, accessToken, { method: "PATCH", body: JSON.stringify({ role }) }); if (!response.ok) { const result = await response.json(); setMessage(result.error?.message ?? "Unable to change role."); return; } setMembers(await loadMembers(accessToken, selectedOrganization)); }
  async function removeMember(member: Member) { if (!accessToken || !selectedOrganization) return; const response = await apiRequest(`/api/v1/organizations/${selectedOrganization.id}/members/${member.id}`, accessToken, { method: "DELETE" }); if (!response.ok) { const result = await response.json(); setMessage(result.error?.message ?? "Unable to remove member."); return; } setMembers(await loadMembers(accessToken, selectedOrganization)); }
  async function logout() { await apiRequest("/api/v1/auth/logout", null, { method: "POST" }); setAccessToken(null); setUser(null); setOrganizations([]); setSelectedOrganization(null); setMessage("You have been signed out."); }

  if (!user) return <main className="min-h-screen bg-slate-100 px-6 py-12 text-slate-950 sm:px-10"><div className="mx-auto grid w-full max-w-5xl gap-12 lg:grid-cols-[1fr_380px] lg:items-center"><section><p className="text-sm font-semibold uppercase tracking-[0.2em] text-teal-700">FlowCart OS</p><h1 className="mt-5 max-w-xl text-4xl font-semibold tracking-tight sm:text-6xl">Multi-Warehouse Order &amp; Fulfilment Platform</h1><p className="mt-6 max-w-lg text-lg leading-8 text-slate-600">A focused operating foundation for coordinating orders and fulfilment across every warehouse.</p></section><section className="bg-white p-7 shadow-sm" aria-labelledby="auth-heading"><div className="flex gap-6 border-b border-slate-200"><button className={`pb-3 text-sm font-semibold ${mode === "login" ? "border-b-2 border-teal-700 text-teal-800" : "text-slate-500"}`} onClick={() => setMode("login")}>Sign in</button><button className={`pb-3 text-sm font-semibold ${mode === "register" ? "border-b-2 border-teal-700 text-teal-800" : "text-slate-500"}`} onClick={() => setMode("register")}>Create account</button></div><h2 id="auth-heading" className="mt-7 text-2xl font-semibold">{mode === "login" ? "Sign in" : "Create your account"}</h2>{message && <p className="mt-4 text-sm text-slate-600" role="status">{message}</p>}<form className="mt-6 space-y-4" onSubmit={submitAuth}><label className="block text-sm font-medium">Email<input className="mt-1 w-full border border-slate-300 px-3 py-2.5" type="email" required value={email} onChange={(event) => setEmail(event.target.value)} /></label><label className="block text-sm font-medium">Password<input className="mt-1 w-full border border-slate-300 px-3 py-2.5" type="password" minLength={8} required value={password} onChange={(event) => setPassword(event.target.value)} /></label>{mode === "register" && <><label className="block text-sm font-medium">First name<input className="mt-1 w-full border border-slate-300 px-3 py-2.5" value={firstName} onChange={(event) => setFirstName(event.target.value)} /></label><label className="block text-sm font-medium">Last name<input className="mt-1 w-full border border-slate-300 px-3 py-2.5" value={lastName} onChange={(event) => setLastName(event.target.value)} /></label></>}<button className="w-full bg-slate-950 px-4 py-3 font-semibold text-white disabled:opacity-60" disabled={loading || !apiUrl}>{loading ? "Please wait..." : mode === "login" ? "Sign in" : "Create account"}</button></form></section></div></main>;

  const canManage = selectedOrganization?.role === "owner" || selectedOrganization?.role === "admin";
  return <main className="min-h-screen bg-slate-100 px-6 py-10 text-slate-950 sm:px-10"><div className="mx-auto max-w-6xl"><header className="flex flex-wrap items-end justify-between gap-5 border-b border-slate-300 pb-6"><div><p className="text-sm font-semibold uppercase tracking-[0.2em] text-teal-700">FlowCart OS</p><h1 className="mt-3 text-3xl font-semibold tracking-tight">Organizations</h1><p className="mt-2 text-slate-600">Signed in as {user.email}</p></div><button className="bg-slate-950 px-4 py-2.5 text-sm font-semibold text-white hover:bg-teal-800" onClick={logout}>Sign out</button></header>{message && <p className="mt-5 text-sm text-slate-600" role="status">{message}</p>}<div className="mt-8 grid gap-8 lg:grid-cols-[220px_1fr]"><aside><h2 className="text-sm font-semibold uppercase tracking-[0.16em] text-slate-500">Your organizations</h2><div className="mt-4 space-y-2">{organizations.map((organization) => <button key={organization.id} className={`block w-full border px-3 py-3 text-left text-sm ${selectedOrganization?.id === organization.id ? "border-teal-700 bg-white" : "border-slate-200 bg-slate-50"}`} onClick={() => setSelectedOrganization(organization)}><span className="block font-semibold">{organization.name}</span><span className="mt-1 block text-xs uppercase text-slate-500">{organization.role}</span></button>)}</div><form className="mt-8 space-y-3 border-t border-slate-300 pt-6" onSubmit={createOrganization}><h2 className="font-semibold">Create organization</h2><input className="w-full border border-slate-300 px-3 py-2" placeholder="Name" required value={organizationName} onChange={(event) => setOrganizationName(event.target.value)} /><input className="w-full border border-slate-300 px-3 py-2" placeholder="slug" required value={organizationSlug} onChange={(event) => setOrganizationSlug(event.target.value)} /><button className="w-full bg-teal-700 px-3 py-2 text-sm font-semibold text-white hover:bg-teal-800" disabled={loading}>Create</button></form></aside>{selectedOrganization ? <section><div className="border-b border-slate-300 pb-5"><p className="text-sm uppercase tracking-[0.16em] text-slate-500">{selectedOrganization.role}</p><h2 className="mt-2 text-3xl font-semibold">{selectedOrganization.name}</h2><p className="mt-1 text-slate-600">/{selectedOrganization.slug}</p></div><div className="mt-8 flex flex-wrap items-end justify-between gap-4"><div><h3 className="text-xl font-semibold">Members</h3><p className="mt-1 text-sm text-slate-600">Membership is the source of truth for access.</p></div>{canManage && <form className="flex flex-wrap gap-2" onSubmit={addMember}><input className="border border-slate-300 px-3 py-2 text-sm" type="email" placeholder="Existing user email" required value={memberEmail} onChange={(event) => setMemberEmail(event.target.value)} /><select className="border border-slate-300 px-3 py-2 text-sm" value={memberRole} onChange={(event) => setMemberRole(event.target.value as Role)}><option value="admin">admin</option><option value="warehouse_manager">warehouse_manager</option><option value="support">support</option><option value="viewer">viewer</option>{selectedOrganization.role === "owner" && <option value="owner">owner</option>}</select><button className="bg-slate-950 px-3 py-2 text-sm font-semibold text-white">Add member</button></form>}</div><div className="mt-5 divide-y divide-slate-200 bg-white">{members.map((member) => <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-4" key={member.id}><div><p className="font-medium">{member.first_name || member.last_name ? `${member.first_name} ${member.last_name}` : member.email}</p><p className="text-sm text-slate-500">{member.email}</p></div><div className="flex items-center gap-3"><select className="border border-slate-300 px-2 py-1 text-sm" disabled={!canManage || (selectedOrganization.role === "admin" && member.role === "owner")} value={member.role} onChange={(event) => changeRole(member, event.target.value as Role)}><option value="owner">owner</option><option value="admin">admin</option><option value="warehouse_manager">warehouse_manager</option><option value="support">support</option><option value="viewer">viewer</option></select>{canManage && !(selectedOrganization.role === "admin" && member.role === "owner") && <button className="text-sm font-semibold text-rose-700" onClick={() => removeMember(member)}>Remove</button>}</div></div>)}</div></section> : <section className="flex min-h-64 items-center justify-center border border-dashed border-slate-300 text-slate-500">Create an organization to get started.</section>}</div></div></main>;
}
