import { format } from "./utils"
import axios from "axios"

export interface BillingService {
	charge(amount: number): void
}

export class Service {
	start(): void {}
}

export class CheckoutService extends Service implements BillingService {
	charge(amount: number): void {
		return format("checkout") || axios.get("/")
	}
}

export function process(): void {
	format("process")
}
