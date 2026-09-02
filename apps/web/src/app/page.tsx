"use client";

import { useEffect, useState } from "react";

const apiUrl = process.env.NEXT_PUBLIC_API_URL ?? "";

export default function Home() {
  const [apiStatus, setApiStatus] = useState<"checking" | "connected" | "unavailable">(
    apiUrl ? "checking" : "unavailable",
  );
  const [serviceName, setServiceName] = useState<string | null>(null);

  useEffect(() => {
    if (!apiUrl) {
      return;
    }

    fetch(`${apiUrl}/health`)
      .then(async (response) => {
        if (!response.ok) {
          throw new Error("Health request failed");
        }

        const health = (await response.json()) as { service?: string };
        setServiceName(health.service ?? null);
        setApiStatus("connected");
      })
      .catch(() => {
        setApiStatus("unavailable");
        setServiceName(null);
      });
  }, []);

  const statusLabel = apiStatus === "connected" ? "API: Connected" : "API: Unavailable";
  const statusColor = apiStatus === "connected" ? "text-emerald-700" : "text-rose-700";

  return (
    <div className="min-h-screen bg-slate-100 text-slate-950">
      <main className="mx-auto flex min-h-screen w-full max-w-5xl flex-col justify-center px-6 py-16 sm:px-10">
        <div className="max-w-3xl">
          <p className="mb-5 text-sm font-semibold uppercase tracking-[0.2em] text-teal-700">FlowCart OS</p>
          <h1 className="text-4xl font-semibold tracking-tight sm:text-6xl">Multi-Warehouse Order &amp; Fulfilment Platform</h1>
          <p className="mt-6 max-w-2xl text-lg leading-8 text-slate-600">
            A focused operating foundation for coordinating orders and fulfilment across every warehouse.
          </p>
        </div>

        <section className="mt-16 max-w-md border-t border-slate-300 pt-6" aria-labelledby="api-status-heading">
          <h2 id="api-status-heading" className="text-sm font-semibold uppercase tracking-[0.16em] text-slate-500">
            API status
          </h2>
          <p className={`mt-3 text-xl font-semibold ${statusColor}`}>
            {apiStatus === "checking" ? "API: Checking" : statusLabel}
          </p>
          {serviceName && <p className="mt-2 text-slate-600">Service: {serviceName}</p>}
        </section>
      </main>
    </div>
  );
}
