import nextWorker from "./.open-next/worker.js";
import { proxyApi } from "./worker/api-proxy.mjs";

type WorkerEnv = {
  API_ORIGIN: string;
  APP_ORIGIN: string;
};
type WorkerExecutionContext = {
  waitUntil(promise: Promise<unknown>): void;
  passThroughOnException(): void;
};
type NextWorker = {
  fetch(request: Request, env: WorkerEnv, context: WorkerExecutionContext): Response | Promise<Response>;
};

const worker = {
  async fetch(request: Request, env: WorkerEnv, context: WorkerExecutionContext) {
    const pathname = new URL(request.url).pathname;
    if (pathname === "/api" || pathname.startsWith("/api/")) return proxyApi(request, env);
    return (nextWorker as NextWorker).fetch(request, env, context);
  },
};

export default worker;
