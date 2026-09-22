"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { ReactNode, useState } from "react";
import { AuthScreen, canOperate, useSession } from "../lib/session";
import { LoadingState } from "./states";

const navigation = [{ href: "/dashboard", label: "Dashboard" }, { href: "/orders", label: "Orders" }, { href: "/inventory", label: "Inventory" }, { href: "/transfers", label: "Transfers" }];

export function ProtectedShell({ children }: { children: ReactNode }) {
  const session = useSession();
  if (session.status === "loading") return <LoadingState label="Restoring your session" />;
  if (session.status === "signed_out") return <AuthScreen />;
  return <AppShell>{children}</AppShell>;
}

function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { user, organization, organizations, selectOrganization, signOut } = useSession();
  const [sidebarOpen, setSidebarOpen] = useState(false);

  async function logout() {
    await signOut();
    router.push("/");
  }

  const visibleNavigation = canOperate(organization?.role) ? [...navigation, ...(organization?.role === "owner" || organization?.role === "admin" ? [{ href: "/audit", label: "Audit Log" }] : [])] : navigation;
  return <div className="app-shell"><aside className={`sidebar ${sidebarOpen ? "open" : ""}`}><div className="brand-lockup"><span className="brand-mark">FC</span><div><strong>FlowCart</strong><span>Operations OS</span></div></div><nav className="primary-nav" aria-label="Primary navigation">{visibleNavigation.map((item) => <Link key={item.href} href={item.href} onClick={() => setSidebarOpen(false)} className={pathname === item.href || pathname.startsWith(`${item.href}/`) ? "active" : ""}>{item.label}</Link>)}</nav><div className="sidebar-note"><span className="eyebrow">M18B</span><p>Workflow completion</p></div></aside>{sidebarOpen && <button className="sidebar-scrim" aria-label="Close navigation" onClick={() => setSidebarOpen(false)} />}<main className="app-main"><header className="topbar"><button className="menu-button" aria-label="Open navigation" onClick={() => setSidebarOpen(true)}>Menu</button><div className="org-context"><span className="muted-label">Workspace</span>{organizations.length > 1 ? <select value={organization?.id ?? ""} onChange={(event) => selectOrganization(event.target.value)} aria-label="Select organization">{organizations.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select> : <strong>{organization?.name ?? "No organization"}</strong>}</div><div className="user-context"><span>{user?.first_name} {user?.last_name}</span><span className="role-pill">{organization?.role ?? "member"}</span><button className="text-button" onClick={logout}>Sign out</button></div></header>{organization ? <div key={organization.id} className="page-content">{children}</div> : <div className="page-content"><div className="state-panel"><strong>No organization selected</strong><span>Your account is not connected to an organization yet.</span></div></div>}</main></div>;
}