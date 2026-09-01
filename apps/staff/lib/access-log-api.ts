import { type AccessLogPage, type AccessLogRequest, accessLogPath } from "./access-log";
import { fetchEventsJSON } from "./events-api";

// The consent access log's fetcher (#569).
//
// A file of its own, separate from the pure module beside it, so that
// access-log.ts stays importable by a framework-free unit test: the act
// vocabulary and the query-string rules are worth testing, and they must not
// drag the API client in behind them.
//
// ONE FETCHER. There is no purge, no retention setting and NO EXPORT — the log
// is read where it lives, because an audit log that can be downloaded is an
// audit log that can be circulated, and downloading it would itself be one of
// the acts it records.

/** One page of the log, newest first. */
export async function fetchAccessLog(request: AccessLogRequest = {}): Promise<AccessLogPage> {
  return fetchEventsJSON<AccessLogPage>(accessLogPath(request));
}
