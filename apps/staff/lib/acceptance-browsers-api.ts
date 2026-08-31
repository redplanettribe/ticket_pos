import {
  type AcceptanceBrowseRequest,
  type CustomerAcceptancePage,
  type LegalDocument,
  type StaffAcceptancePage,
  acceptanceBrowseBody,
  customerBrowserPath,
  staffBrowserPath,
} from "./acceptance-browsers";
import { fetchEventsJSON } from "./events-api";

// The two acceptance browsers' fetchers (#565).
//
// A file of their own, separate from the pure module beside it, so that
// acceptance-browsers.ts stays importable by a framework-free unit test: the
// vocabulary, the defaults and the body-building are rules worth testing, and
// they must not drag the API client in behind them.
//
// BOTH ARE POSTS THAT READ. Nothing here creates, records or changes anything.
// The verb is POST because a data subject's email must never reach a URL, a
// query string or a referer — and two of the parameters are addresses: the
// search fragment, and the cursor, which under keyset paging on `email ASC` is
// the last address of the page just served.

/** One page of the customer browser. */
export async function fetchCustomerAcceptances(
  document: LegalDocument,
  request: AcceptanceBrowseRequest,
): Promise<CustomerAcceptancePage> {
  return fetchEventsJSON<CustomerAcceptancePage>(customerBrowserPath(document), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(acceptanceBrowseBody(request)),
  });
}

/** One page of the staff browser. */
export async function fetchStaffAcceptances(
  request: AcceptanceBrowseRequest,
): Promise<StaffAcceptancePage> {
  return fetchEventsJSON<StaffAcceptancePage>(staffBrowserPath(), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(acceptanceBrowseBody(request)),
  });
}
