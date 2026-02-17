/** API error wrapper with HTTP status code metadata. */
export class APIError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/**
 * Converts a failed fetch response into a normalized APIError.
 */
export async function toAPIError(response: Response): Promise<APIError> {
  const fallback = `${response.status} ${
    response.statusText || "Request failed"
  }`;
  const contentType = response.headers.get("Content-Type") ?? "";
  try {
    if (contentType.includes("application/json")) {
      const payload = await response.json();
      if (payload && typeof payload.error === "string") {
        return new APIError(response.status, payload.error);
      }
    }
    const bodyText = await response.text();
    if (bodyText.trim() !== "") {
      return new APIError(response.status, bodyText.trim());
    }
  } catch (_error) {
    // Fall back to a generic message.
  }
  return new APIError(response.status, fallback);
}

/**
 * Extracts a readable message from unknown errors.
 */
export function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message !== "") {
    return err.message;
  }
  return "Unexpected error";
}

/**
 * Fetches a text endpoint and returns its plain body.
 */
export async function getText(path: string): Promise<string> {
  const response = await fetch(path, {
    method: "GET",
    headers: { Accept: "text/plain, application/json" },
  });
  if (!response.ok) {
    throw await toAPIError(response);
  }
  return await response.text();
}

/**
 * Sends text data to a writable endpoint.
 */
export async function putText(
  path: string,
  content: string,
  contentType: string,
): Promise<void> {
  const response = await fetch(path, {
    method: "PUT",
    headers: { "Content-Type": contentType },
    body: content,
  });
  if (!response.ok) {
    throw await toAPIError(response);
  }
}
