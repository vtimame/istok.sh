package cli

type TaskCommand struct {
	List TaskListCommand `cmd:"" help:"List open tasks in the current project."`
	Show TaskShowCommand `cmd:"" help:"Show one task from the current project."`
}

type TaskListCommand struct {
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
}

type TaskShowCommand struct {
	ID       int64  `arg:"" name:"ID" required:"" help:"Project-scoped task number in the current project."`
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
}
