-- The human-readable card instrument the Payment Provider reports at Confirm
-- (e.g. "visa ····1234"), kept on the Payment for platform-side support
-- lookups. Empty when the provider sent none (wallet payments, the stub).
ALTER TABLE payments ADD COLUMN instrument TEXT;
