import type { RefundService } from "./refund-service.js";

export class RefundController {
  constructor(private readonly refunds: RefundService) {}

  async postRefund(paymentId: string, requestId: string) {
    const refund = await this.refunds.createRefund(paymentId, requestId);

    return { status: 200, body: refund };
  }
}
