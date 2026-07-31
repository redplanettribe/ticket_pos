"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { toast } from "@ticket-pos/ui";

import { applyAuthFork } from "@/lib/auth-fork";

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

import { SUPPORTED_CURRENCIES, formatPriceCents } from "@/lib/events-api";
import { payableBalanceExplanation } from "@/lib/payable-balance";
import { formatPaidAtDate } from "@/lib/payouts";

import { OrgLogoImage } from "./org-logo-image";
import { PayoutProfileForm } from "./payout-profile-form";

type Organization = {
  id: string;
  name: string;
  slug: string;
  currency: string;
  currency_locked: boolean;
  logo_url: string | null;
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

type Payout = {
  id: string;
  amount_cents: number;
  /** A calendar day ("YYYY-MM-DD"), not an instant. */
  paid_at: string;
  note: string | null;
};

type PayoutsSummary = {
  /** What the platform owes. Signed: negative after a post-settlement reversal. */
  withdrawable_balance_cents: number;
  /**
   * The part of it that has cleared and may be asked for today (ADR 0025).
   * Signed too, never larger than the figure above, and negative when a
   * settlement got ahead of what had cleared.
   */
  payable_balance_cents: number;
  currency: string;
  payouts: Payout[];
};

type Assignment = {
  id: string;
  member_id: string;
  email: string;
  role: string;
};

type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new Error(envelope.error?.message ?? "Request failed");
  }
  if (envelope.data === null) {
    throw new Error("Empty response");
  }
  return envelope.data;
}

const memberRoleLabel: Record<string, string> = {
  org_admin: "Org Admin",
  event_owner: "Event Owner",
  event_staff: "Event Staff",
};

export function SettingsPageClient() {
  const router = useRouter();
  const [organization, setOrganization] = useState<Organization | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [payouts, setPayouts] = useState<PayoutsSummary | null>(null);
  const [assignmentsByEvent, setAssignmentsByEvent] = useState<Record<string, Assignment[]>>({});
  const [expandedEventId, setExpandedEventId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [profileName, setProfileName] = useState("");
  const [profileCurrency, setProfileCurrency] = useState("USD");
  const [currencyLocked, setCurrencyLocked] = useState(false);
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
      const [org, memberList, eventList, payoutsSummary] = await Promise.all([
        fetchJSON<Organization>("/api/settings/organization"),
        fetchJSON<Member[]>("/api/settings/members"),
        fetchJSON<Event[]>("/api/settings/events"),
        fetchJSON<PayoutsSummary>("/api/settings/organization/payouts"),
      ]);
      setOrganization(org);
      setPayouts(payoutsSummary);
      setProfileName(org.name);
      setProfileCurrency(org.currency);
      setCurrencyLocked(org.currency_locked);
      setMembers(memberList);
      setEvents(eventList);
      setForbidden(false);
    } catch (loadError) {
      const message = loadError instanceof Error ? loadError.message : "Failed to load settings";
      if (message.toLowerCase().includes("permission")) {
        setForbidden(true);
      } else {
        setError(message);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  async function saveProfile() {
    try {
      const org = await fetchJSON<Organization>("/api/settings/organization", {
        method: "PATCH",
        body: JSON.stringify({ name: profileName, currency: profileCurrency }),
      });
      setOrganization(org);
      setProfileCurrency(org.currency);
      setCurrencyLocked(org.currency_locked);
      toast.success("Organization profile updated");
      router.refresh();
    } catch (saveError) {
      toast.error(saveError instanceof Error ? saveError.message : "Failed to save");
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
      toast.success("Member added");
    } catch (addError) {
      toast.error(addError instanceof Error ? addError.message : "Failed to add member");
    }
  }

  async function updateMemberRole(memberId: string, role: string) {
    try {
      await fetchJSON<Member>(`/api/settings/members/${memberId}`, {
        method: "PATCH",
        body: JSON.stringify({ role }),
      });
      await loadAll();
      toast.success("Member role updated");
    } catch (updateError) {
      toast.error(updateError instanceof Error ? updateError.message : "Failed to update role");
    }
  }

  async function removeMember(memberId: string) {
    try {
      await fetchJSON<{ message: string }>(`/api/settings/members/${memberId}`, {
        method: "DELETE",
      });
      await loadAll();
      toast.success("Member removed");
    } catch (removeError) {
      toast.error(removeError instanceof Error ? removeError.message : "Failed to remove member");
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
      toast.success("Event created");
    } catch (createError) {
      toast.error(createError instanceof Error ? createError.message : "Failed to create event");
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
      toast.success("Event assignment saved");
    } catch (assignError) {
      toast.error(assignError instanceof Error ? assignError.message : "Failed to assign member");
    }
  }

  async function removeAssignment(eventId: string, memberId: string) {
    try {
      await fetchJSON<{ message: string }>(`/api/settings/events/${eventId}/assignments/${memberId}`, {
        method: "DELETE",
      });
      await loadAssignments(eventId);
      toast.success("Assignment removed");
    } catch (removeError) {
      toast.error(removeError instanceof Error ? removeError.message : "Failed to remove assignment");
    }
  }

  async function deleteOrganization() {
    try {
      await fetchJSON<{ message: string }>("/api/settings/organization", {
        method: "DELETE",
        body: JSON.stringify({ confirmation_name: deleteConfirmation }),
      });
      toast.success("Organization deleted");

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
      if (fork.error) {
        toast.error(fork.error);
      }
      router.push(fork.path);
      router.refresh();
    } catch (deleteError) {
      toast.error(deleteError instanceof Error ? deleteError.message : "Failed to delete organization");
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading settings...</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Access denied</AlertTitle>
        <AlertDescription>You need Org Admin access to manage organization settings.</AlertDescription>
      </Alert>
    );
  }

  if (error || !organization) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Could not load settings</AlertTitle>
        <AlertDescription>{error ?? "Unknown error"}</AlertDescription>
      </Alert>
    );
  }

  const assignableMembers = members.filter((member) => member.role !== "org_admin");

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Profile</CardTitle>
          <CardDescription>Organization display name, currency, and Storefront URL slug.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <FormField id="org-name" label="Organization name">
            <Input value={profileName} onChange={(event) => setProfileName(event.target.value)} />
          </FormField>
          <FormField
            id="org-currency"
            label="Currency"
            description={
              currencyLocked
                ? "Locked after the first ticket type is created in this organization."
                : "Set before creating ticket types. Used for all ticket prices."
            }
          >
            <select
              id="org-currency"
              className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60"
              value={profileCurrency}
              onChange={(event) => setProfileCurrency(event.target.value)}
              disabled={currencyLocked}
            >
              {SUPPORTED_CURRENCIES.map((currency) => (
                <option key={currency} value={currency}>
                  {currency}
                </option>
              ))}
            </select>
          </FormField>
          <FormField
            id="org-slug"
            label="Slug"
            description="Immutable after creation. Used in Storefront URLs."
          >
            <Input value={organization.slug} readOnly className="bg-muted" />
          </FormField>
          <Button type="button" onClick={() => void saveProfile()}>
            Save profile
          </Button>
        </CardContent>
      </Card>

      <OrgLogoImage
        organizationName={organization.name}
        logoUrl={organization.logo_url}
        onUpdated={(logoUrl) => setOrganization((current) => (current ? { ...current, logo_url: logoUrl } : current))}
      />

      <PayoutProfileForm />

      {payouts ? (
        <Card>
          <CardHeader>
            <CardTitle>Payouts</CardTitle>
            <CardDescription>
              What your online sales have earned, less what has already been paid out to you.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            {/*
              Both balances, side by side, with the gap between them in words.
              Showing only the smaller figure would be simpler and would send
              every organizer who sold this morning to support asking why they
              are being offered less than they earned (ADR 0025).
            */}
            <div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div>
                  <p className="text-sm text-muted-foreground">Total balance</p>
                  <p className="text-3xl font-semibold tabular-nums">
                    {formatPriceCents(payouts.withdrawable_balance_cents, payouts.currency)}
                  </p>
                  <p className="text-xs text-muted-foreground">Everything your online sales have earned you so far.</p>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">Available to request</p>
                  <p className="text-3xl font-semibold tabular-nums">
                    {formatPriceCents(payouts.payable_balance_cents, payouts.currency)}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Sales clear overnight, so today&apos;s are not here yet.
                  </p>
                </div>
              </div>
              <p className="mt-4 text-sm text-muted-foreground">
                {payableBalanceExplanation(
                  payouts.withdrawable_balance_cents,
                  payouts.payable_balance_cents,
                  (cents) => formatPriceCents(cents, payouts.currency),
                )}
              </p>
            </div>

            <div className="space-y-3">
              <p className="text-sm font-medium">Payout history</p>
              {payouts.payouts.length === 0 ? (
                <p className="text-sm text-muted-foreground">No payouts recorded yet.</p>
              ) : (
                <div className="space-y-3">
                  {payouts.payouts.map((payout) => (
                    <div
                      key={payout.id}
                      className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
                    >
                      <div>
                        <p className="font-medium tabular-nums">
                          {formatPriceCents(payout.amount_cents, payouts.currency)}
                        </p>
                        {payout.note ? (
                          <p className="text-sm text-muted-foreground">{payout.note}</p>
                        ) : null}
                      </div>
                      <p className="text-sm text-muted-foreground">{formatPaidAtDate(payout.paid_at)}</p>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Members</CardTitle>
          <CardDescription>Pre-provision members by email. They can sign in with OTP immediately.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="grid gap-4 md:grid-cols-[1fr_auto_auto] md:items-end">
            <FormField id="member-email" label="Email">
              <Input
                type="email"
                value={memberEmail}
                onChange={(event) => setMemberEmail(event.target.value)}
                placeholder="staff@example.com"
              />
            </FormField>
            <FormField id="member-role" label="Role">
              <select
                id="member-role"
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={memberRole}
                onChange={(event) => setMemberRole(event.target.value)}
              >
                <option value="org_admin">Org Admin</option>
                <option value="event_owner">Event Owner</option>
                <option value="event_staff">Event Staff</option>
              </select>
            </FormField>
            <Button type="button" onClick={() => void addMember()}>
              Add member
            </Button>
          </div>

          <div className="space-y-3">
            {members.map((member) => (
              <div
                key={member.id}
                className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
              >
                <div>
                  <p className="font-medium">{member.email}</p>
                  <p className="text-sm text-muted-foreground">
                    Joined {new Date(member.created_at).toLocaleDateString()}
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <select
                    aria-label={`Role for ${member.email}`}
                    className="h-10 rounded-md border border-input bg-background px-3 text-sm"
                    value={member.role}
                    onChange={(event) => void updateMemberRole(member.id, event.target.value)}
                  >
                    <option value="org_admin">Org Admin</option>
                    <option value="event_owner">Event Owner</option>
                    <option value="event_staff">Event Staff</option>
                  </select>
                  <Button type="button" variant="outline" onClick={() => setMemberToRemove(member)}>
                    Remove
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Event access</CardTitle>
          <CardDescription>Assign members to Events with per-event roles.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="grid gap-4 md:grid-cols-[1fr_1fr_auto] md:items-end">
            <FormField id="event-name" label="Event name">
              <Input value={eventName} onChange={(event) => setEventName(event.target.value)} />
            </FormField>
            <FormField id="event-slug" label="Event slug">
              <Input value={eventSlug} onChange={(event) => setEventSlug(event.target.value)} />
            </FormField>
            <Button type="button" onClick={() => void createEvent()}>
              Create event
            </Button>
          </div>

          {events.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No Events yet. Create an Event above to manage assignments.
            </p>
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
                        <p className="font-medium">{event.name}</p>
                        <p className="text-sm text-muted-foreground">/{event.slug}</p>
                      </div>
                      <span className="text-sm text-muted-foreground">{expanded ? "Hide" : "Manage"}</span>
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
                                {memberRoleLabel[assignment.role] ?? assignment.role}
                              </span>
                              <Button
                                type="button"
                                variant="outline"
                                onClick={() => void removeAssignment(event.id, assignment.member_id)}
                              >
                                Remove
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
                                  Org role: {memberRoleLabel[member.role] ?? member.role}
                                </p>
                              </div>
                              <div className="flex items-center gap-2">
                                <select
                                  aria-label={`Assignment role for ${member.email}`}
                                  className="h-10 rounded-md border border-input bg-background px-3 text-sm"
                                  defaultValue={existing?.role ?? "event_staff"}
                                  onChange={(changeEvent) =>
                                    void assignMember(event.id, member.id, changeEvent.target.value)
                                  }
                                >
                                  <option value="event_owner">Event Owner</option>
                                  <option value="event_staff">Event Staff</option>
                                </select>
                              </div>
                            </div>
                          );
                        })}
                        {assignableMembers.length === 0 ? (
                          <p className="text-sm text-muted-foreground">
                            Add non-admin Members before assigning Event access.
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
          <CardTitle>Danger zone</CardTitle>
          <CardDescription>
            Permanently delete this Organization, its Members, Events, and assignments.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button type="button" variant="destructive" onClick={() => setDeleteOpen(true)}>
            Delete organization
          </Button>
        </CardContent>
      </Card>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete organization</DialogTitle>
            <DialogDescription>
              Type <strong>{organization.name}</strong> to confirm. This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <FormField id="delete-confirmation" label="Confirmation">
            <Input
              value={deleteConfirmation}
              onChange={(event) => setDeleteConfirmation(event.target.value)}
              placeholder={organization.name}
            />
          </FormField>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteConfirmation !== organization.name}
              onClick={() => void deleteOrganization()}
            >
              Delete organization
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={memberToRemove !== null} onOpenChange={(open) => !open && setMemberToRemove(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove member</DialogTitle>
            <DialogDescription>
              Remove <strong>{memberToRemove?.email}</strong> from this Organization? They will lose access
              immediately.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setMemberToRemove(null)}>
              Cancel
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
              Remove member
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
