package graphstore

import "fmt"

var (
	errRepositoryNil      = fmt.Errorf("graphstore: repository is nil")
	errTransactionNil     = fmt.Errorf("graphstore: transaction is required")
	errLookupRequestEmpty = fmt.Errorf("graphstore: lookup request is empty")
	errPathRequestMissing = fmt.Errorf("graphstore: path request is missing nodes")
)

func errLookupRequestMissing() error {
	return errLookupRequestEmpty
}
