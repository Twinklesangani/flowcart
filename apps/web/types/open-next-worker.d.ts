declare module "*.open-next/worker.js" {
  const worker: {
    fetch(request: Request, env: Record<string, unknown>, context: {
      waitUntil(promise: Promise<unknown>): void;
      passThroughOnException(): void;
    }): Response | Promise<Response>;
  };

  export default worker;
}