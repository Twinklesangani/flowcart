"use client";

import { AuthScreen, useSession } from "../lib/session";

export default function Home() {
  const { status } = useSession();
  if (status === "loading" || status === "authenticated") return null;
  return <AuthScreen />;
}