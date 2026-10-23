export async function secureWorkerResponse(request, next) {
  const requestURL = new URL(request.url);
  if (requestURL.protocol === "http:") {
    requestURL.protocol = "https:";
    return Response.redirect(requestURL.toString(), 308);
  }

  const response = await next();
  const headers = new Headers(response.headers);
  headers.set("Strict-Transport-Security", "max-age=31536000");
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}
