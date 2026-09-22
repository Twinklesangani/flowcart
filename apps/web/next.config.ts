import type { NextConfig } from "next";
import { initOpenNextCloudflareForDev } from "@opennextjs/cloudflare";

const development = process.env.NODE_ENV === "development";
const appEnv = process.env.NEXT_PUBLIC_APP_ENV?.trim().toLowerCase() || "development";
const apiUrl = process.env.NEXT_PUBLIC_API_URL?.trim();
let apiOrigin = "";
if (apiUrl) {
  try {
    const parsed = new URL(apiUrl);
    if (!["http:", "https:"].includes(parsed.protocol) || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
      throw new Error();
    }
    apiOrigin = parsed.origin;
  } catch {
    throw new Error("NEXT_PUBLIC_API_URL must be an HTTP(S) origin without credentials, paths, queries or fragments.");
  }
}

// Static App Router pages include inline hydration scripts. Nonces would require
// changing rendering to dynamic. Keep that current contract; eval is dev-only.
const csp = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline'${development ? " 'unsafe-eval'" : ""}`,
  // Built-in Next.js 404/global-error HTML contains inline styles in production.
  "style-src 'self' 'unsafe-inline'",
  `connect-src 'self'${apiOrigin ? ` ${apiOrigin}` : ""}${development ? " ws://localhost:3000 ws://localhost:3001" : ""}`,
  "img-src 'self' data:",
  "font-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join("; ");

const nextConfig: NextConfig = {
  env: { NEXT_PUBLIC_APP_ENV: appEnv },
  async headers() {
    return [{ source: "/:path*", headers: [
      { key: "Content-Security-Policy", value: csp },
      { key: "X-Content-Type-Options", value: "nosniff" },
      { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
      { key: "X-Frame-Options", value: "DENY" },
      { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
    ] }];
  },
};

export default nextConfig;

initOpenNextCloudflareForDev();
