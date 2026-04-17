package repos

import "fmt"

func unexpectedQuerierCall(method string) error {
	return fmt.Errorf("unexpected %s call", method)
}
