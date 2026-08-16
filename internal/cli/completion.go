package cli

import (
	"errors"
	"fmt"

	"github.com/alecthomas/kong"
	kongcompletion "github.com/jotaen/kong-completion"
)

type CompletionCommand = kongcompletion.Completion

// registerCompletion handles shell completion requests before normal argument
// parsing can construct an Fx graph or open SQLite.
func registerCompletion(parser *kong.Kong) (handled bool, err error) {
	var completionErr error

	kongcompletion.Register(
		parser,
		kongcompletion.WithErrorHandler(func(value error) {
			completionErr = errors.Join(completionErr, value)
		}),
		kongcompletion.WithExitFunc(func(code int) {
			handled = true
			if code != 0 && completionErr == nil {
				completionErr = fmt.Errorf("completion exited with code %d", code)
			}
		}),
	)
	if completionErr != nil {
		return handled, fmt.Errorf("run shell completion: %w", completionErr)
	}

	return handled, nil
}
