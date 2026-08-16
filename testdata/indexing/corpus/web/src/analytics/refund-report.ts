export function buildRefundIdempotencyReport(requestIds: string[]): string {
  return `refund requestId idempotency report: ${requestIds.join(",")}`;
}
