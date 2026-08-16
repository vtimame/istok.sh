export type Refund = {
  id: string;
  paymentId: string;
  requestId: string;
};

export interface RefundRepository {
  findByRequestId(requestId: string): Promise<Refund | undefined>;
  save(refund: Refund): Promise<void>;
}
