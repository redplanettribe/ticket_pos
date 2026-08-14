"use client";

import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { toast } from "@ticket-pos/ui";

import { apiErrorMessage, fieldErrorMessages } from "@/lib/api-errors";
import { applyAuthFork } from "@/lib/auth-fork";
import { formatDate, PLATFORM_TIME_ZONE } from "@/lib/format";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Input,
} from "@ticket-pos/ui";

import { ApiError, SUPPORTED_CURRENCIES } from "@/lib/events-api";
import { ASSIGNMENT_ROLES, MEMBER_ROLES, useRoleName } from "@/app/role-name";

import { OrgLogoImage } from "./org-logo-image";

type Organization = {
  id: string;
  name: string;
  slug: string;
  currency: string;
  currency_locked: boolean;
  logo_url: string | null;
  // The Support WhatsApp number in canonical E.164 form, null when the
  // Organization has none. Published on every one of its Event pages (ADR 0029).
  support_whatsapp: string | null;
};

type Member = {
  id: string;
  email: string;
  role: string;
  created_at: string;
};

type Event = {
  id: string;
  name: string;
  slug: string;
};

type Assignment = {
  id: string;
  member_id: string;
  email: string;
  role: string;
};

type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

/**
 * Every call this surface makes, failing as an `ApiError` rather than as a bare
 * `Error` carrying a sentence.
 *
 * This used to throw `new Error(envelope.error.message)` and every catch below
 * rendered that string. That threw away the one part of a refusal this app can
 * translate — the `code` — and left a Spanish reader reading English at exactly
 * the moment something went wrong (ADR 0023, ADR 0041). The code, the message
 * and the details now travel together, so `apiErrorMessage` can pick this app's
 * words when it has them and fall back to the API's English when it does not.
 */
async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new ApiError(
      envelope.error?.message ?? "",
      envelope.error?.code,
      envelope.error?.details as Record<string, unknown> | undefined,
    );
  }
  if (envelope.data === null) {
    throw new ApiError("");
  }
  return envelope.data;
}

/** The `error` an `apiErrorMessage` call wants, out of whatever was caught. */
function caughtApiError(error: unknown): ApiError | null {
  return error instanceof ApiError ? error : null;
}

export function SettingsPageClient() {
  const t = useTranslations("organization");
  const tTeam = useTranslations("team");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const roleName = useRoleName();
  const router = useRouter();
  const [organization, setOrganization] = useState<Organization | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [assignmentsByEvent, setAssignmentsByEvent] = useState<Record<string, Assignment[]>>({});
  const [expandedEventId, setExpandedEventId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [profileName, setProfileName] = useState("");
  const [profileCurrency, setProfileCurrency] = useState("USD");
  const [currencyLocked, setCurrencyLocked] = useState(false);
  const [profileSupportWhatsApp, setProfileSupportWhatsApp] = useState("");
  // The API's verdict on the Support WhatsApp number, keyed on the field name it
  // travels under, so a rejected number says what is wrong with it beside the
  // input rather than only as "check the details above" in a toast.
  const [profileFieldErrors, setProfileFieldErrors] = useState<Record<string, string>>({});
  const [memberEmail, setMemberEmail] = useState("");
  const [memberRole, setMemberRole] = useState("event_staff");
  const [eventName, setEventName] = useState("");
  const [eventSlug, setEventSlug] = useState("");
  const [deleteConfirmation, setDeleteConfirmation] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [memberToRemove, setMemberToRemove] = useState<Member | null>(null);

  const loadAssignments = useCallback(async (eventId: string) => {
    const data = await fetchJSON<Assignment[]>(`/api/settings/events/${eventId}/assignments`);
    setAssignmentsByEvent((current) => ({ ...current, [eventId]: data }));
  }, []);

  const loadAll = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [org, memberList, eventList] = await Promise.all([
        fetchJSON<Organization>("/api/settings/organization"),
        fetchJSON<Member[]>("/api/settings/members"),
        fetchJSON<Event[]>("/api/settings/events"),
      ]);
      setOrganization(org);
      setProfileName(org.name);
      setProfileCurrency(org.currency);
      setCurrencyLocked(org.currency_locked);
      setProfileSupportWhatsApp(org.support_whatsapp ?? "");
      setMembers(memberList);
      setEvents(eventList);
      setForbidden(false);
    } catch (loadError) {
      const apiError = caughtApiError(loadError);
      // The API's own code, not a substring of its English sentence. The old
      // test here was `message.includes("permission")`, which stopped being true
      // the moment anything but English could be shown (ADR 0041).
      if (apiError?.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(apiErrorMessage(errorCopy, apiError) ?? t("loadFailed"));
      }
    } finally {
      setLoading(false);
    }
  }, [errorCopy, t]);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  async function saveProfile() {
    setProfileFieldErrors({});
    try {
      const org = await fetchJSON<Organization>("/api/settings/organization", {
        method: "PATCH",
        body: JSON.stringify({
          name: profileName,
          currency: profileCurrency,
          // Always sent, so an emptied field reads as "withdraw the number". The
          // API distinguishes an absent key from a blank one — absent leaves the
          // stored number alone — but this form always has an opinion about it.
          support_whatsapp: profileSupportWhatsApp,
        }),
      });
      setOrganization(org);
      setProfileCurrency(org.currency);
      setCurrencyLocked(org.currency_locked);
      // Show back what was actually stored: the server canonicalises the number,
      // so an Org Admin who typed "+593 (0)98-765.4321" sees "+593987654321" and
      // knows exactly what their Customers will reach.
      setProfileSupportWhatsApp(org.support_whatsapp ?? "");
      toast.success(t("profileSavedToast"));
      router.refresh();
    } catch (saveError) {
      const apiError = caughtApiError(saveError);
      setProfileFieldErrors(fieldErrorMessages(errorCopy, apiError?.details));
      toast.error(apiErrorMessage(errorCopy, apiError) ?? t("profileSaveFailed"));
    }
  }

  async function addMember() {
    try {
      await fetchJSON<Member>("/api/settings/members", {
        method: "POST",
        body: JSON.stringify({ email: memberEmail, role: memberRole }),
      });
      setMemberEmail("");
      await loadAll();
      toast.success(tTeam("memberAddedToast"));
    } catch (addError) {
      toast.error(apiErrorMessage(errorCopy, caughtApiError(addError)) ?? tTeam("addMemberFailed"));
    }
  }

  async function updateMemberRole(memberId: string, role: string) {
    try {
      await fetchJSON<Member>(`/api/settings/members/${memberId}`, {
        method: "PATCH",
        body: JSON.stringify({ role }),
      });
      await loadAll();
      toast.success(tTeam("roleUpdatedToast"));
    } catch (updateError) {
      toast.error(
        apiErrorMessage(errorCopy, caughtApiError(updateError)) ?? tTeam("updateRoleFailed"),
      );
    }
  }

  async function removeMember(memberId: string) {
    try {
      await fetchJSON<{ message: string }>(`/api/settings/members/${memberId}`, {
        method: "DELETE",
      });
      await loadAll();
      toast.success(tTeam("memberRemovedToast"));
    } catch (removeError) {
      toast.error(
        apiErrorMessage(errorCopy, caughtApiError(removeError)) ?? tTeam("removeMemberFailed"),
      );
    }
  }

  async function createEvent() {
    try {
      await fetchJSON<Event>("/api/settings/events", {
        method: "POST",
        body: JSON.stringify({ name: eventName, slug: eventSlug }),
      });
      setEventName("");
      setEventSlug("");
      await loadAll();
      toast.success(tTeam("eventCreatedToast"));
    } catch (createError) {
      toast.error(
        apiErrorMessage(errorCopy, caughtApiError(createError)) ?? tTeam("createEventFailed"),
      );
    }
  }

  async function toggleEvent(eventId: string) {
    if (expandedEventId === eventId) {
      setExpandedEventId(null);
      return;
    }
    setExpandedEventId(eventId);
    await loadAssignments(eventId);
  }

  async function assignMember(eventId: string, memberId: string, role: string) {
    try {
      await fetchJSON<Assignment>(`/api/settings/events/${eventId}/assignments/${memberId}`, {
        method: "PUT",
        body: JSON.stringify({ role }),
      });
      await loadAssignments(eventId);
      toast.success(tTeam("assignmentSavedToast"));
    } catch (assignError) {
      toast.error(apiErrorMessage(errorCopy, caughtApiError(assignError)) ?? tTeam("assignFailed"));
    }
  }

  async function removeAssignment(eventId: string, memberId: string) {
    try {
      await fetchJSON<{ message: string }>(
        `/api/settings/events/${eventId}/assignments/${memberId}`,
        { method: "DELETE" },
      );
      await loadAssignments(eventId);
      toast.success(tTeam("assignmentRemovedToast"));
    } catch (removeError) {
      toast.error(
        apiErrorMessage(errorCopy, caughtApiError(removeError)) ?? tTeam("removeAssignmentFailed"),
      );
    }
  }

  async function deleteOrganization() {
    try {
      await fetchJSON<{ message: string }>("/api/settings/organization", {
        method: "DELETE",
        body: JSON.stringify({ confirmation_name: deleteConfirmation }),
      });
      toast.success(t("deletedToast"));

      const sessionResponse = await fetch("/api/auth/session");
      const sessionEnvelope = (await sessionResponse.json()) as APIEnvelope<{
        active_member: unknown | null;
        memberships: Array<{ member_id: string }>;
      }>;
      const session = sessionEnvelope.data;
      if (!session) {
        router.push("/login");
        router.refresh();
        return;
      }

      const fork = await applyAuthFork(session);
      if (fork.failure) {
        // `applyAuthFork` hands back the API's error rather than a sentence of
        // its own, so the code resolves here the way it does on every other
        // migrated surface. The fallback is the shell's, because choosing an
        // Organization is the shell's business and it already words it.
        toast.error(
          apiErrorMessage(errorCopy, fork.failure.apiError) ?? t("selectOrganizationFailed"),
        );
      }
      router.push(fork.path);
      router.refresh();
    } catch (deleteError) {
      toast.error(apiErrorMessage(errorCopy, caughtApiError(deleteError)) ?? t("deleteFailed"));
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("loading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDeniedDescription")}</AlertDescription>
      </Alert>
    );
  }

  if (error || !organization) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
        <AlertDescription>{error ?? t("loadFailed")}</AlertDescription>
      </Alert>
    );
  }

  const assignableMembers = members.filter((member) => member.role !== "org_admin");

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>{t("profileTitle")}</CardTitle>
          <CardDescription>{t("profileDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <FormField id="org-name" label={t("nameLabel")}>
            <Input value={profileName} onChange={(event) => setProfileName(event.target.value)} />
          </FormField>
          <FormField
            id="org-currency"
            label={t("currencyLabel")}
            description={
              currencyLocked ? t("currencyLockedDescription") : t("currencyDescription")
            }
          >
            <select
              id="org-currency"
              className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60"
              value={profileCurrency}
              onChange={(event) => setProfileCurrency(event.target.value)}
              disabled={currencyLocked}
            >
              {/*
                A currency code is not copy: USD is USD in both languages, and it
                is what the Organization's money is stated in (CONTEXT.md).
              */}
              {SUPPORTED_CURRENCIES.map((currency) => (
                <option key={currency} value={currency}>
                  {currency}
                </option>
              ))}
            </select>
          </FormField>
          <FormField
            id="org-support-whatsapp"
            label={t("supportWhatsAppLabel")}
            description={t("supportWhatsAppDescription")}
            error={profileFieldErrors.support_whatsapp}
          >
            <Input
              id="org-support-whatsapp"
              type="tel"
              placeholder={t("supportWhatsAppPlaceholder")}
              value={profileSupportWhatsApp}
              onChange={(event) => setProfileSupportWhatsApp(event.target.value)}
            />
          </FormField>
          <FormField id="org-slug" label={t("slugLabel")} description={t("slugDescription")}>
            <Input value={organization.slug} readOnly className="bg-muted" />
          </FormField>
          <Button type="button" onClick={() => void saveProfile()}>
            {t("saveProfile")}
          </Button>
        </CardContent>
      </Card>

      <OrgLogoImage
        organizationName={organization.name}
        logoUrl={organization.logo_url}
        onUpdated={(logoUrl) =>
          setOrganization((current) => (current ? { ...current, logo_url: logoUrl } : current))
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>{tTeam("membersTitle")}</CardTitle>
          <CardDescription>{tTeam("membersDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="grid gap-4 md:grid-cols-[1fr_auto_auto] md:items-end">
            <FormField id="member-email" label={tTeam("emailLabel")}>
              <Input
                type="email"
                value={memberEmail}
                onChange={(event) => setMemberEmail(event.target.value)}
                placeholder={tTeam("emailPlaceholder")}
              />
            </FormField>
            <FormField id="member-role" label={tTeam("roleLabel")}>
              <select
                id="member-role"
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={memberRole}
                onChange={(event) => setMemberRole(event.target.value)}
              >
                {MEMBER_ROLES.map((role) => (
                  <option key={role} value={role}>
                    {roleName(role)}
                  </option>
                ))}
              </select>
            </FormField>
            <Button type="button" onClick={() => void addMember()}>
              {tTeam("addMember")}
            </Button>
          </div>

          <div className="space-y-3">
            {members.map((member) => (
              <div
                key={member.id}
                className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
              >
                <div>
                  {/* An email address is data: it reads as it was typed, in both languages. */}
                  <p className="font-medium">{member.email}</p>
                  <p className="text-sm text-muted-foreground">
                    {tTeam("joined", {
                      date: formatDate(member.created_at, PLATFORM_TIME_ZONE, locale) ?? "",
                    })}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <select
                    aria-label={tTeam("roleForMember", { email: member.email })}
                    className="h-10 rounded-md border border-input bg-background px-3 text-sm"
                    value={member.role}
                    onChange={(event) => void updateMemberRole(member.id, event.target.value)}
                  >
                    {MEMBER_ROLES.map((role) => (
                      <option key={role} value={role}>
                        {roleName(role)}
                      </option>
                    ))}
                  </select>
                  <Button type="button" variant="outline" onClick={() => setMemberToRemove(member)}>
                    {tTeam("remove")}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{tTeam("eventAccessTitle")}</CardTitle>
          <CardDescription>{tTeam("eventAccessDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="grid gap-4 md:grid-cols-[1fr_1fr_auto] md:items-end">
            <FormField id="event-name" label={tTeam("eventNameLabel")}>
              <Input value={eventName} onChange={(event) => setEventName(event.target.value)} />
            </FormField>
            <FormField id="event-slug" label={tTeam("eventSlugLabel")}>
              <Input value={eventSlug} onChange={(event) => setEventSlug(event.target.value)} />
            </FormField>
            <Button type="button" onClick={() => void createEvent()}>
              {tTeam("createEvent")}
            </Button>
          </div>

          {events.length === 0 ? (
            <p className="text-sm text-muted-foreground">{tTeam("noEvents")}</p>
          ) : (
            <div className="space-y-3">
              {events.map((event) => {
                const assignments = assignmentsByEvent[event.id] ?? [];
                const expanded = expandedEventId === event.id;
                return (
                  <div key={event.id} className="rounded-md border">
                    <button
                      type="button"
                      className="flex w-full items-center justify-between px-4 py-3 text-left"
                      onClick={() => void toggleEvent(event.id)}
                    >
                      <div>
                        {/* An Event's name and slug are the Organization's own words. */}
                        <p className="font-medium">{event.name}</p>
                        <p className="text-sm text-muted-foreground">/{event.slug}</p>
                      </div>
                      <span className="text-sm text-muted-foreground">
                        {expanded ? tTeam("hide") : tTeam("manage")}
                      </span>
                    </button>
                    {expanded ? (
                      <div className="space-y-4 border-t px-4 py-4">
                        {assignments.map((assignment) => (
                          <div
                            key={assignment.id}
                            className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"
                          >
                            <p>{assignment.email}</p>
                            <div className="flex items-center gap-2">
                              <span className="text-sm text-muted-foreground">
                                {roleName(assignment.role)}
                              </span>
                              <Button
                                type="button"
                                variant="outline"
                                onClick={() => void removeAssignment(event.id, assignment.member_id)}
                              >
                                {tTeam("remove")}
                              </Button>
                            </div>
                          </div>
                        ))}
                        {assignableMembers.map((member) => {
                          const existing = assignments.find((item) => item.member_id === member.id);
                          return (
                            <div
                              key={member.id}
                              className="flex flex-col gap-2 rounded-md bg-muted/40 p-3 sm:flex-row sm:items-center sm:justify-between"
                            >
                              <div>
                                <p className="font-medium">{member.email}</p>
                                <p className="text-sm text-muted-foreground">
                                  {tTeam("orgRole", { role: roleName(member.role) })}
                                </p>
                              </div>
                              <div className="flex items-center gap-2">
                                <select
                                  aria-label={tTeam("assignmentRoleForMember", {
                                    email: member.email,
                                  })}
                                  className="h-10 rounded-md border border-input bg-background px-3 text-sm"
                                  defaultValue={existing?.role ?? "event_staff"}
                                  onChange={(changeEvent) =>
                                    void assignMember(event.id, member.id, changeEvent.target.value)
                                  }
                                >
                                  {ASSIGNMENT_ROLES.map((role) => (
                                    <option key={role} value={role}>
                                      {roleName(role)}
                                    </option>
                                  ))}
                                </select>
                              </div>
                            </div>
                          );
                        })}
                        {assignableMembers.length === 0 ? (
                          <p className="text-sm text-muted-foreground">
                            {tTeam("noAssignableMembers")}
                          </p>
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="border-destructive/40">
        <CardHeader>
          <CardTitle>{t("dangerZoneTitle")}</CardTitle>
          <CardDescription>{t("dangerZoneDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button type="button" variant="destructive" onClick={() => setDeleteOpen(true)}>
            {t("deleteOrganization")}
          </Button>
        </CardContent>
      </Card>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("deleteOrganization")}</DialogTitle>
            <DialogDescription>
              {t.rich("deleteDialogDescription", {
                name: organization.name,
                em: (chunks) => <strong>{chunks}</strong>,
              })}
            </DialogDescription>
          </DialogHeader>
          <FormField id="delete-confirmation" label={t("confirmationLabel")}>
            <Input
              value={deleteConfirmation}
              onChange={(event) => setDeleteConfirmation(event.target.value)}
              placeholder={organization.name}
            />
          </FormField>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteOpen(false)}>
              {t("cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteConfirmation !== organization.name}
              onClick={() => void deleteOrganization()}
            >
              {t("deleteOrganization")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={memberToRemove !== null}
        onOpenChange={(open) => !open && setMemberToRemove(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{tTeam("removeMemberTitle")}</DialogTitle>
            <DialogDescription>
              {tTeam.rich("removeMemberDescription", {
                email: memberToRemove?.email ?? "",
                em: (chunks) => <strong>{chunks}</strong>,
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setMemberToRemove(null)}>
              {tTeam("cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={() => {
                if (!memberToRemove) {
                  return;
                }
                void removeMember(memberToRemove.id);
                setMemberToRemove(null);
              }}
            >
              {tTeam("removeMemberConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
