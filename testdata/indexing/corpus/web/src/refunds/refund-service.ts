import type { RefundMailer } from "../notifications/refund-mailer.js";
import type { Refund, RefundRepository } from "./refund-repository.js";

export class RefundService {
  constructor(
    private readonly repository: RefundRepository,
    private readonly mailer: RefundMailer,
  ) {}

  async createRefund(paymentId: string, requestId: string): Promise<Refund> {
    const existing = await this.repository.findByRequestId(requestId);
    if (existing) {
      return existing;
    }

    const refund: Refund = {
      id: `refund-${requestId}`,
      paymentId,
      requestId,
    };

    await this.repository.save(refund);
    await this.mailer.sendRefundCreated(refund);

    return refund;
  }
}
