package main

// transcript is what the website animates: the user's prompt, then the
// agent's steps in order, each with the output it produced.
type transcript struct {
	CWD        string `json:"cwd"`
	Prompt     string `json:"prompt"`
	TaskNumber int64  `json:"task_number"`
	Steps      []step `json:"steps"`
}

type stepKind string

const (
	// stepIstok is an Istok MCP tool call.
	stepIstok stepKind = "istok"
	// stepTool is one of the agent's own tools, such as reading or editing a file.
	stepTool stepKind = "tool"
	// stepMessage is the agent's closing reply to the user.
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
