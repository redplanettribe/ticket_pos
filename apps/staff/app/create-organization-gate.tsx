"use client";

import { AuthCard } from "@ticket-pos/ui";

import { LogoutButton } from "@/app/logout-button";

import { CreateOrganizationForm } from "./create-organization-form";

export function CreateOrganizationGate() {
  return (
    <AuthCard
      title="Create your organization"
      description="Name your venue and choose a URL slug for your storefront."
      footer={<LogoutButton />}
    >
      <CreateOrganizationForm />
    </AuthCard>
  );
}
