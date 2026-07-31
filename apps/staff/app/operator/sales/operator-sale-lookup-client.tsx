"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import {
  Button,
  Card,
  CardContent,
  FormField,
  Input,
  PageHeader,
} from "@ticket-pos/ui";

/**
 * The door a support thread opens: paste a Sale Confirmation reference and land
 * on that sale, whichever Organization it belongs to (#124).
 *
 * There is no sales browser here and none is planned — the flow always starts
 * from a reference somebody was given, so with nothing entered this page shows
 * the search and nothing else (#193). The field validates nothing beyond being
 * non-empty: whether a reference names a sale is the API's answer, and the
 * lookup ignores case, so a reference quoted in lowercase resolves too.
 */
export function OperatorSaleLookupClient() {
  const router = useRouter();
  const [reference, setReference] = useState("");

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    const trimmed = reference.trim();
    if (!trimmed) {
      return;
    }
    router.push(`/operator/sales/${encodeURIComponent(trimmed)}`);
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Find a sale"
        description="Look a ticket sale up by its sale confirmation reference, across every organization."
      />

      <Card>
        <CardContent>
          <form className="grid gap-4 sm:grid-cols-[2fr_auto] sm:items-end" onSubmit={handleSubmit}>
            <FormField id="operator-sale-reference" label="Sale confirmation reference">
              <Input
                value={reference}
                onChange={(event) => setReference(event.target.value)}
                placeholder="TP-J7K2QX9M"
                autoComplete="off"
                spellCheck={false}
              />
            </FormField>
            <Button type="submit" disabled={!reference.trim()}>
              Find sale
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
