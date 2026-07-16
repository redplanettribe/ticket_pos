type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

function apiBaseUrl(): string {
  const url = process.env.API_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  return url.replace(/\/$/, "");
}

export type PublicOrganization = {
  name: string;
  slug: string;
  logo_url: string | null;
};

export async function getPublicOrganization(slug: string): Promise<PublicOrganization | null> {
  try {
    const response = await fetch(`${apiBaseUrl()}/api/v1/public/organizations/${encodeURIComponent(slug)}`, {
      cache: "no-store",
    });
    const envelope = (await response.json()) as APIEnvelope<PublicOrganization>;
    if (!response.ok || envelope.error) {
      return null;
    }
    return envelope.data;
  } catch {
    return null;
  }
}
