import { ApiClient, Organization, User } from "./api";

export const logoutStorageKey = "flowcart.explicit-logout";
export type LogoutStorage = {
  read: () => string | null;
  write: (value: string) => void;
  clear: () => void;
};

// Fail closed on automatic hydration if browser storage is unavailable.
export const browserLogoutStorage: LogoutStorage = {
  read: () => { try { return window.localStorage.getItem(logoutStorageKey); } catch { return "storage-unavailable"; } },
  write: (value) => { try { window.localStorage.setItem(logoutStorageKey, value); } catch { /* Memory invalidation still applies. */ } },
  clear: () => { try { window.localStorage.removeItem(logoutStorageKey); } catch { /* Reload remains signed out. */ } },
};

type Snapshot = {
  status: "loading" | "authenticated" | "signed_out";
  user: User | null;
  accessToken: string | null;
  organizations: Organization[];
  organization: Organization | null;
  sessionNotice: string;
};
const initialSnapshot: Snapshot = { status: "loading", user: null, accessToken: null, organizations: [], organization: null, sessionNotice: "" };

// The provider uses this same state machine; tests can control network completion
// order without a DOM or timing sleeps. Credentials never enter storage.
export class SessionController {
  private snapshot = initialSnapshot;
  private generation = 0;
  private listeners = new Set<() => void>();
  private logoutPromise: Promise<void> | null = null;

  constructor(readonly client: ApiClient, private storage: LogoutStorage) {}
  getSnapshot = () => this.snapshot;
  getServerSnapshot = () => initialSnapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };

  private update(values: Partial<Snapshot>) {
    this.snapshot = { ...this.snapshot, ...values };
    this.listeners.forEach((listener) => listener());
  }

  private clearLocalSession = (notice = "") => {
    this.generation++;
    this.client.invalidateSession();
    this.update({ ...initialSnapshot, status: "signed_out", sessionNotice: notice });
  };

  private installGuard() { this.client.setRefreshGuard(() => this.storage.read() !== null); }

  private async hydrate(token: string, generation: number) {
    if (generation !== this.generation) return;
    this.client.setSession(token, (nextToken) => {
      if (generation === this.generation && this.storage.read() === null) this.update({ accessToken: nextToken });
    }, () => this.clearLocalSession());
    this.update({ accessToken: token });
    try {
      const [user, result] = await Promise.all([
        this.client.request<User>("/api/v1/auth/me"),
        this.client.request<{ organizations: Organization[] }>("/api/v1/organizations"),
      ]);
      if (generation !== this.generation) return;
      this.update({ user, organizations: result.organizations, organization: result.organizations.find((item) => item.id === this.snapshot.organization?.id) ?? result.organizations[0] ?? null, status: "authenticated", sessionNotice: "" });
    } catch (error) {
      if (generation === this.generation) this.clearLocalSession();
      throw error;
    }
  }

  // Cleanup cancels only this subscriber, never the shared refresh request.
  restore = () => {
    this.installGuard();
    let cancelled = false;
    if (this.storage.read() !== null) {
      this.clearLocalSession();
      return () => { cancelled = true; };
    }
    this.client.setSession(null);
    const generation = this.generation;
    void this.client.refreshSession().then((result) => {
      if (!cancelled && generation === this.generation && this.storage.read() === null) return this.hydrate(result.access_token, generation);
    }).catch(() => { if (!cancelled && generation === this.generation) this.clearLocalSession(); });
    return () => { cancelled = true; };
  };

  onStorage = (event: Pick<StorageEvent, "key" | "newValue">) => {
    // Clearing the marker never automatically signs another tab in.
    if (event.key === logoutStorageKey && event.newValue !== null) this.clearLocalSession();
  };

  signIn = async (mode: "login" | "register", values: Record<string, string>) => {
    this.installGuard();
    const generation = ++this.generation;
    const marker = this.storage.read();
    this.client.invalidateSession();
    // An older response must not overwrite the new login's cookie.
    await this.logoutPromise;
    await this.client.waitForRefreshes();
    if (generation !== this.generation || marker !== this.storage.read()) return;
    const result = await this.client.request<{ access_token: string }>(`/api/v1/auth/${mode}`, { method: "POST", body: JSON.stringify(values) }, false);
    if (generation !== this.generation || marker !== this.storage.read()) return;
    this.storage.clear();
    await this.hydrate(result.access_token, generation);
  };

  signOut = (): Promise<void> => {
    // A unique, non-secret value also notifies tabs on repeated logout attempts.
    this.storage.write(crypto.randomUUID());
    this.clearLocalSession();
    const generation = this.generation;
    if (this.logoutPromise) return this.logoutPromise;
    const pending = this.client.request<void>("/api/v1/auth/logout", { method: "POST", signal: AbortSignal.timeout(15000) }, false)
      .catch(() => { if (generation === this.generation) this.update({ sessionNotice: "Signed out locally, but server session revocation could not be confirmed." }); });
    this.logoutPromise = pending;
    void pending.finally(() => { if (this.logoutPromise === pending) this.logoutPromise = null; });
    return pending;
  };

  selectOrganization = (id: string) => {
    if (this.snapshot.status === "authenticated") this.update({ organization: this.snapshot.organizations.find((item) => item.id === id) ?? null });
  };
}
