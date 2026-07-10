import createClient from "openapi-fetch";
import type { paths } from "./generated/schema";

export type { paths } from "./generated/schema";
export type APIClient = ReturnType<typeof createAPIClient>;

export function createAPIClient(baseUrl: string) {
  return createClient<paths>({ baseUrl });
}
