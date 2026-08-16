package cli

type TaskCommand struct {
	List TaskListCommand `cmd:"" help:"List open tasks in the current project."`
}

type TaskListCommand struct {
	Database string `name:"database" help:"Path to the SQLite database." env:"ISTOK_DATABASE"`
}
