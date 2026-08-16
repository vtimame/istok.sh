import type { Refund } from "../refunds/refund-repository.js";

export interface RefundMailer {
  sendRefundCreated(refund: Refund): Promise<void>;
}
