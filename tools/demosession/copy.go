package main

import "fmt"

// copyText holds what people and agents say in the sessions. Istok's own
// output stays in English, as it is in a real terminal; the user writes in
// their language and the agents answer in it.
type copyText struct {
	workPrompt     string
	workTitle      string
	workAcceptance string
	workSummary    string
	workNote       string
	workReply      func(task int64) string

	memoryPrompt string
	memoryTitle  string
	memoryBody   string
	memoryReply  string

	followPrompt string
	followTitle  string
	followReply  string
}

var copies = map[string]copyText{
	"en": {
		workPrompt:     "Reject orders with a zero or negative amount.",
		workTitle:      "Reject orders with a non-positive amount",
		workAcceptance: "CreateOrder rejects zero and negative amounts without charging the customer.",
		workSummary:    "CreateOrder rejects zero and negative amounts with ErrInvalidAmount before charging; covered by a test.",
		workNote:       "Non-positive amounts are rejected before any charge.",
		workReply: func(task int64) string {
			return fmt.Sprintf("Done. Orders with a zero or negative amount are now rejected before any charge, and go test passes. "+
				"Everything is recorded in Istok as task #%d: open istok ui or run istok task show %d.", task, task)
		},

		memoryPrompt: "Remember for this project: API timestamps are always UTC, in RFC 3339.",
		memoryTitle:  "API timestamps are UTC in RFC 3339",
		memoryBody:   "Every timestamp the API returns or accepts is UTC and formatted as RFC 3339, e.g. 2026-10-04T13:27:00Z.",
		memoryReply:  "Saved as a project rule. Every agent that picks up a task in acme-api will get it.",

		followPrompt: "Add created_at to the GET /orders/{id} response.",
		followTitle:  "Add created_at to the order response",
		followReply:  "Following the project rule: created_at goes out in UTC as RFC 3339.",
	},

	"ru": {
		workPrompt:     "Отклоняй заказы с нулевой или отрицательной суммой.",
		workTitle:      "Отклонять заказы с неположительной суммой",
		workAcceptance: "CreateOrder отклоняет нулевые и отрицательные суммы и не списывает деньги.",
		workSummary:    "CreateOrder отклоняет нулевые и отрицательные суммы через ErrInvalidAmount до списания; покрыто тестом.",
		workNote:       "Неположительные суммы отклоняются до любого списания.",
		workReply: func(task int64) string {
			return fmt.Sprintf("Готово. Заказы с нулевой или отрицательной суммой теперь отклоняются до списания, go test проходит. "+
				"Всё записано в Istok как задача #%d: откройте istok ui или выполните istok task show %d.", task, task)
		},

		memoryPrompt: "Запомни для этого проекта: время в API всегда в UTC, в формате RFC 3339.",
		memoryTitle:  "Время в API — UTC в формате RFC 3339",
		memoryBody:   "Все метки времени, которые API отдаёт и принимает, — в UTC и в формате RFC 3339, например 2026-10-04T13:27:00Z.",
		memoryReply:  "Сохранил как правило проекта. Его получит каждый агент, который возьмёт задачу в acme-api.",

		followPrompt: "Добавь created_at в ответ GET /orders/{id}.",
		followTitle:  "Добавить created_at в ответ заказа",
		followReply:  "Учитываю правило проекта: created_at отдаю в UTC в формате RFC 3339.",
	},
}
