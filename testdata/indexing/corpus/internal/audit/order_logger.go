package audit

import "log"

func LogOrderEvent(orderID, message string) {
	log.Printf("order idempotency audit: order=%s message=%s", orderID, message)
}
