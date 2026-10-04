package main

// transcript is what the website animates: one or more agent sessions in the
// same terminal, each with the user's prompt and the agent's steps in order.
type transcript struct {
	Parts []part `json:"parts"`
}

type part struct {
	// Agent is the command the user starts in the shell, such as claude.
	Agent  string `json:"agent"`
	CWD    string `json:"cwd"`
	Prompt string `json:"prompt"`
	Steps  []step `json:"steps"`
}

type stepKind string

const (
	// stepIstok is an Istok MCP tool call.
	stepIstok stepKind = "istok"
	// stepTool is one of the agent's own tools, such as reading or editing a file.
	stepTool stepKind = "tool"
	// stepMessage is the agent's reply to the user.
	stepMessage stepKind = "message"
)

type step struct {
	Kind    stepKind `json:"kind"`
	Name    string   `json:"name,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Output  []line   `json:"output,omitempty"`
	Text    string   `json:"text,omitempty"`
}

type lineTone string

const (
	tonePlain   lineTone = ""
	toneMuted   lineTone = "muted"
	toneSuccess lineTone = "success"
	toneAdded   lineTone = "added"
)

type line struct {
	Text string   `json:"text"`
	Tone lineTone `json:"tone,omitempty"`
}
